//! Command-line and environment configuration.

use crate::{epoch::EpochCache, rate_limit::RateLimiter, server::RequestLimits};
use anyhow::Context;
use clap::Parser;
use doublezero_config::Environment;
use ipnetwork::IpNetwork;
use solana_keypair::{read_keypair_file, Keypair};
use std::{net::SocketAddr, path::PathBuf, sync::Arc, time::Duration};

/// Every flag also reads a `DZ_IP_VERIFIER_`-prefixed environment variable, which is how the
/// service is configured in a container.
#[derive(Debug, Parser)]
#[command(
    term_width = 0,
    name = "doublezero-ip-verifier",
    about = "Signs the source IP it observes, as an RFC-27 IpOwnershipProof",
    version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION"))
)]
pub struct AppArgs {
    /// DoubleZero environment, used for the default ledger RPC URL.
    #[arg(long, env = "DZ_IP_VERIFIER_ENV", default_value = "devnet", value_parser = parse_environment)]
    pub env: Environment,

    /// DoubleZero Ledger RPC URL. Defaults to the URL for `--env`.
    #[arg(long, env = "DZ_IP_VERIFIER_LEDGER_RPC")]
    pub ledger_rpc: Option<String>,

    /// Path to the verifier keypair JSON file. Its public key must match
    /// `GlobalState.ip_verifier_authority_pk` or every proof this service issues is rejected
    /// onchain.
    #[arg(long, env = "DZ_IP_VERIFIER_KEYPAIR")]
    pub keypair: PathBuf,

    /// HTTP listen address.
    #[arg(
        long,
        env = "DZ_IP_VERIFIER_LISTEN_ADDR",
        default_value = "0.0.0.0:8080"
    )]
    pub listen_addr: SocketAddr,

    /// Prometheus metrics listen address. Kept off the public listener: the metrics port is for
    /// scraping from inside the deployment, not for clients.
    #[arg(
        long,
        env = "DZ_IP_VERIFIER_METRICS_ADDR",
        default_value = "127.0.0.1:2112"
    )]
    pub metrics_addr: SocketAddr,

    /// Log filter.
    #[arg(
        long,
        env = "DZ_IP_VERIFIER_LOG",
        default_value = "doublezero_ip_verifier=info"
    )]
    pub log: String,

    /// CIDR whose connections may carry a forwarded client address. Repeatable. With none set,
    /// forwarded headers are ignored entirely and the connection peer address is used — the correct
    /// setting for a service clients reach directly.
    #[arg(
        long = "trusted-proxy",
        env = "DZ_IP_VERIFIER_TRUSTED_PROXIES",
        value_delimiter = ',',
        value_name = "CIDR"
    )]
    pub trusted_proxies: Vec<IpNetwork>,

    /// Seconds between ledger epoch refreshes.
    #[arg(long, env = "DZ_IP_VERIFIER_EPOCH_REFRESH_SECS", default_value = "10")]
    pub epoch_refresh_secs: u64,

    /// Age at which a cached epoch stops being signed with. Past this the service refuses requests
    /// rather than issuing proofs dated to an epoch it can no longer vouch for.
    #[arg(long, env = "DZ_IP_VERIFIER_MAX_EPOCH_AGE_SECS", default_value = "120")]
    pub max_epoch_age_secs: u64,

    /// Requests one source address may make back to back.
    #[arg(long, env = "DZ_IP_VERIFIER_RATE_LIMIT_BURST", default_value = "5")]
    pub rate_limit_burst: u32,

    /// Sustained requests per minute per source address.
    #[arg(
        long,
        env = "DZ_IP_VERIFIER_RATE_LIMIT_PER_MINUTE",
        default_value = "30"
    )]
    pub rate_limit_per_minute: u32,

    /// Source addresses tracked for rate limiting before idle entries are dropped.
    #[arg(
        long,
        env = "DZ_IP_VERIFIER_RATE_LIMIT_MAX_ENTRIES",
        default_value = "100000"
    )]
    pub rate_limit_max_entries: usize,

    /// Maximum request body size in bytes. A proof request is two short fields.
    #[arg(long, env = "DZ_IP_VERIFIER_MAX_BODY_BYTES", default_value = "1024")]
    pub max_body_bytes: usize,

    /// Per-request timeout in seconds.
    #[arg(long, env = "DZ_IP_VERIFIER_REQUEST_TIMEOUT_SECS", default_value = "5")]
    pub request_timeout_secs: u64,
}

/// `Environment`'s own `FromStr` yields an `eyre::Report`, which clap cannot consume directly.
fn parse_environment(value: &str) -> Result<Environment, String> {
    value.parse::<Environment>().map_err(|err| err.to_string())
}

impl AppArgs {
    /// Reads the verifier keypair. The error deliberately names only the path: the file's contents
    /// must never reach a log line.
    pub fn keypair(&self) -> anyhow::Result<Arc<Keypair>> {
        read_keypair_file(&self.keypair).map(Arc::new).map_err(|_| {
            anyhow::anyhow!("could not read a keypair from {}", self.keypair.display())
        })
    }

    pub fn ledger_rpc_url(&self) -> anyhow::Result<String> {
        match &self.ledger_rpc {
            Some(url) => Ok(url.clone()),
            None => Ok(self
                .env
                .config()
                .map_err(|err| anyhow::anyhow!("{err}"))
                .with_context(|| format!("no ledger RPC URL for environment {}", self.env))?
                .ledger_public_rpc_url),
        }
    }

    pub fn epoch_cache(&self) -> EpochCache {
        EpochCache::new(Duration::from_secs(self.max_epoch_age_secs))
    }

    pub fn rate_limiter(&self) -> RateLimiter {
        RateLimiter::new(
            self.rate_limit_burst,
            self.rate_limit_per_minute,
            self.rate_limit_max_entries,
        )
    }

    pub fn request_limits(&self) -> RequestLimits {
        RequestLimits {
            max_body_bytes: self.max_body_bytes,
            timeout: Duration::from_secs(self.request_timeout_secs),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::IpAddr;

    fn parse(extra: &[&str]) -> AppArgs {
        let mut argv = vec!["doublezero-ip-verifier", "--keypair", "/dev/null"];
        argv.extend_from_slice(extra);
        AppArgs::try_parse_from(argv).expect("args parse")
    }

    #[test]
    fn defaults_trust_no_proxies() {
        assert!(parse(&[]).trusted_proxies.is_empty());
    }

    #[test]
    fn trusted_proxies_accept_repeated_flags_and_comma_lists() {
        let args = parse(&[
            "--trusted-proxy",
            "10.0.0.0/8,192.168.1.0/24",
            "--trusted-proxy",
            "2001:db8::/64",
        ]);

        assert_eq!(args.trusted_proxies.len(), 3);
        assert!(args.trusted_proxies[0].contains(IpAddr::from([10, 1, 2, 3])));
        assert!(args.trusted_proxies[2].contains("2001:db8::5".parse::<IpAddr>().unwrap()));
    }

    #[test]
    fn an_invalid_cidr_is_rejected() {
        assert!(AppArgs::try_parse_from([
            "doublezero-ip-verifier",
            "--keypair",
            "/dev/null",
            "--trusted-proxy",
            "not-a-cidr",
        ])
        .is_err());
    }

    #[test]
    fn the_ledger_rpc_url_defaults_to_the_environment() {
        let args = parse(&["--env", "testnet"]);
        assert_eq!(
            args.ledger_rpc_url().unwrap(),
            Environment::Testnet.config().unwrap().ledger_public_rpc_url
        );
    }

    #[test]
    fn an_explicit_ledger_rpc_url_wins() {
        let args = parse(&["--env", "testnet", "--ledger-rpc", "http://localhost:8899"]);
        assert_eq!(args.ledger_rpc_url().unwrap(), "http://localhost:8899");
    }

    #[test]
    fn a_missing_keypair_file_reports_the_path_and_nothing_else() {
        let args = AppArgs::try_parse_from([
            "doublezero-ip-verifier",
            "--keypair",
            "/nonexistent/verifier.json",
        ])
        .unwrap();

        let err = args.keypair().expect_err("missing file is an error");
        assert_eq!(
            err.to_string(),
            "could not read a keypair from /nonexistent/verifier.json"
        );
    }
}
