//! Resolved, read-only configuration shared between the binary and every
//! module crate.
//!
//! Per RFC-20 (§CliContext): the binary populates `CliContext` once at
//! startup from `--env` plus any explicit flag or environment-variable
//! overrides. Modules treat the value as read-only. The context carries
//! resolved values only — URLs, paths, identifiers, the signer path, and
//! the output-format hint — never live clients.

use serde::{Deserialize, Serialize};
use solana_sdk::pubkey::Pubkey;
use std::path::PathBuf;

use doublezero_config::Environment;

/// The output-format hint carried by `CliContext`.
///
/// Verbs continue to own their own `--json` / `--json-compact` flags per RFC
/// §Output ("per-command `--json` keeps coupling to the binary low"). This
/// hint exists so the binary can communicate a default when a verb chooses to
/// honor it.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
pub enum OutputFormat {
    #[default]
    Table,
    Json,
    JsonCompact,
}

/// Resolved configuration carried from the binary into every module verb.
#[derive(Debug, Clone)]
pub struct CliContext {
    /// Selected environment (e.g., `Devnet`).
    pub env: Environment,

    /// DZ ledger RPC URL (HTTPS).
    pub ledger_rpc_url: String,
    /// DZ ledger WebSocket URL.
    pub ledger_ws_rpc_url: String,
    /// Solana L1 RPC URL.
    pub solana_l1_rpc_url: String,

    /// Serviceability program ID.
    pub serviceability_program_id: Pubkey,
    /// Geolocation program ID.
    pub geolocation_program_id: Pubkey,
    /// Telemetry program ID.
    pub telemetry_program_id: Pubkey,

    /// Path to the signer keypair file, if provided.
    ///
    /// Modules construct their own `Keypair` from this path lazily; the
    /// context never holds keypair material directly (RFC-20 §Security).
    pub keypair_path: Option<PathBuf>,

    /// Daemon Unix socket path, if provided.
    pub daemon_socket_path: Option<PathBuf>,

    /// Default output-format hint.
    pub output_format: OutputFormat,
}

/// Builder for `CliContext`. The binary populates a builder from parsed
/// global flags and per-field overrides; the builder applies `--env`-derived
/// defaults from `doublezero-config` and yields a fully resolved context.
#[derive(Debug, Default)]
pub struct CliContextBuilder {
    env: Option<Environment>,
    ledger_rpc_url: Option<String>,
    ledger_ws_rpc_url: Option<String>,
    solana_l1_rpc_url: Option<String>,
    serviceability_program_id: Option<Pubkey>,
    geolocation_program_id: Option<Pubkey>,
    telemetry_program_id: Option<Pubkey>,
    keypair_path: Option<PathBuf>,
    daemon_socket_path: Option<PathBuf>,
    output_format: OutputFormat,
}

impl CliContextBuilder {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn with_env(mut self, env: Environment) -> Self {
        self.env = Some(env);
        self
    }

    pub fn with_ledger_rpc_url(mut self, url: impl Into<String>) -> Self {
        self.ledger_rpc_url = Some(url.into());
        self
    }

    pub fn with_ledger_ws_rpc_url(mut self, url: impl Into<String>) -> Self {
        self.ledger_ws_rpc_url = Some(url.into());
        self
    }

    pub fn with_solana_l1_rpc_url(mut self, url: impl Into<String>) -> Self {
        self.solana_l1_rpc_url = Some(url.into());
        self
    }

    pub fn with_serviceability_program_id(mut self, id: Pubkey) -> Self {
        self.serviceability_program_id = Some(id);
        self
    }

    pub fn with_geolocation_program_id(mut self, id: Pubkey) -> Self {
        self.geolocation_program_id = Some(id);
        self
    }

    pub fn with_telemetry_program_id(mut self, id: Pubkey) -> Self {
        self.telemetry_program_id = Some(id);
        self
    }

    pub fn with_keypair_path(mut self, path: PathBuf) -> Self {
        self.keypair_path = Some(path);
        self
    }

    pub fn with_daemon_socket_path(mut self, path: PathBuf) -> Self {
        self.daemon_socket_path = Some(path);
        self
    }

    pub fn with_output_format(mut self, format: OutputFormat) -> Self {
        self.output_format = format;
        self
    }

    /// Resolve all fields and produce a `CliContext`.
    ///
    /// If `env` is set and a given override is `None`, the corresponding
    /// value is sourced from the `doublezero-config` `NetworkConfig` for that
    /// environment. If `env` is unset, the caller must supply every URL and
    /// program-ID field explicitly.
    ///
    /// When the caller supplies `ledger_rpc_url` but not `ledger_ws_rpc_url`,
    /// the WebSocket URL is derived from the RPC URL by scheme swap
    /// (`https → wss`, `http → ws`) so that a custom RPC override is not
    /// silently paired with a stale env-default WS URL.
    pub fn build(self) -> eyre::Result<CliContext> {
        let Some(env) = self.env else {
            return self.build_without_env();
        };
        let config = env.config()?;

        let ledger_rpc_url_override = self.ledger_rpc_url.is_some();
        let ledger_rpc_url = self.ledger_rpc_url.unwrap_or(config.ledger_public_rpc_url);
        let ledger_ws_rpc_url = match self.ledger_ws_rpc_url {
            Some(ws) => ws,
            None if ledger_rpc_url_override => derive_ws_from_rpc(&ledger_rpc_url),
            None => config.ledger_public_ws_rpc_url,
        };

        Ok(CliContext {
            env,
            ledger_rpc_url,
            ledger_ws_rpc_url,
            solana_l1_rpc_url: self.solana_l1_rpc_url.unwrap_or(config.solana_l1_rpc_url),
            serviceability_program_id: self
                .serviceability_program_id
                .unwrap_or(config.serviceability_program_id),
            geolocation_program_id: self
                .geolocation_program_id
                .unwrap_or(config.geolocation_program_id),
            telemetry_program_id: self
                .telemetry_program_id
                .unwrap_or(config.telemetry_program_id),
            keypair_path: self.keypair_path,
            daemon_socket_path: self.daemon_socket_path,
            output_format: self.output_format,
        })
    }

    fn build_without_env(self) -> eyre::Result<CliContext> {
        let ledger_rpc_url = self
            .ledger_rpc_url
            .ok_or_else(|| eyre::eyre!("ledger_rpc_url is required when env is unset"))?;
        let ledger_ws_rpc_url = self
            .ledger_ws_rpc_url
            .unwrap_or_else(|| derive_ws_from_rpc(&ledger_rpc_url));
        let solana_l1_rpc_url = self
            .solana_l1_rpc_url
            .ok_or_else(|| eyre::eyre!("solana_l1_rpc_url is required when env is unset"))?;
        let serviceability_program_id = self.serviceability_program_id.ok_or_else(|| {
            eyre::eyre!("serviceability_program_id is required when env is unset")
        })?;
        let geolocation_program_id = self
            .geolocation_program_id
            .ok_or_else(|| eyre::eyre!("geolocation_program_id is required when env is unset"))?;
        let telemetry_program_id = self
            .telemetry_program_id
            .ok_or_else(|| eyre::eyre!("telemetry_program_id is required when env is unset"))?;

        Ok(CliContext {
            env: Environment::default(),
            ledger_rpc_url,
            ledger_ws_rpc_url,
            solana_l1_rpc_url,
            serviceability_program_id,
            geolocation_program_id,
            telemetry_program_id,
            keypair_path: self.keypair_path,
            daemon_socket_path: self.daemon_socket_path,
            output_format: self.output_format,
        })
    }
}

fn derive_ws_from_rpc(rpc: &str) -> String {
    if let Some(rest) = rpc.strip_prefix("https://") {
        return format!("wss://{rest}");
    }
    if let Some(rest) = rpc.strip_prefix("http://") {
        return format!("ws://{rest}");
    }
    rpc.to_string()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serial_test::serial;

    #[test]
    #[serial]
    fn builder_resolves_from_env_defaults() {
        let ctx = CliContextBuilder::new()
            .with_env(Environment::MainnetBeta)
            .build()
            .unwrap();
        assert_eq!(ctx.env, Environment::MainnetBeta);
        assert!(ctx.ledger_rpc_url.starts_with("https://"));
        assert!(ctx.ledger_ws_rpc_url.starts_with("wss://"));
        assert!(!ctx.solana_l1_rpc_url.is_empty());
        assert_eq!(ctx.output_format, OutputFormat::Table);
        assert!(ctx.keypair_path.is_none());
    }

    #[test]
    #[serial]
    fn builder_per_field_overrides_win_over_env() {
        let ctx = CliContextBuilder::new()
            .with_env(Environment::Devnet)
            .with_ledger_rpc_url("https://custom-rpc.example/")
            .build()
            .unwrap();
        // Per-field overrides win over env. Binaries are free to forbid this
        // combination at the CLI layer (see `client/doublezero`); the builder
        // itself stays permissive so library callers can mix env-derived
        // defaults with targeted overrides.
        assert_eq!(ctx.ledger_rpc_url, "https://custom-rpc.example/");
        // The WS URL is derived from the custom RPC, not left at devnet's
        // default — otherwise the resolved context would be inconsistent.
        assert_eq!(ctx.ledger_ws_rpc_url, "wss://custom-rpc.example/");
    }

    #[test]
    #[serial]
    fn builder_derives_wss_from_https_rpc_override() {
        let ctx = CliContextBuilder::new()
            .with_env(Environment::Devnet)
            .with_ledger_rpc_url("https://custom-rpc.example/")
            .build()
            .unwrap();
        assert_eq!(ctx.ledger_rpc_url, "https://custom-rpc.example/");
        assert_eq!(ctx.ledger_ws_rpc_url, "wss://custom-rpc.example/");
    }

    #[test]
    #[serial]
    fn builder_derives_ws_from_http_rpc_override() {
        let ctx = CliContextBuilder::new()
            .with_env(Environment::Devnet)
            .with_ledger_rpc_url("http://localhost:8899/")
            .build()
            .unwrap();
        assert_eq!(ctx.ledger_rpc_url, "http://localhost:8899/");
        assert_eq!(ctx.ledger_ws_rpc_url, "ws://localhost:8899/");
    }

    #[test]
    #[serial]
    fn builder_explicit_ws_wins_over_derivation() {
        let ctx = CliContextBuilder::new()
            .with_env(Environment::Devnet)
            .with_ledger_rpc_url("https://custom-rpc.example/")
            .with_ledger_ws_rpc_url("wss://other-ws.example/")
            .build()
            .unwrap();
        assert_eq!(ctx.ledger_ws_rpc_url, "wss://other-ws.example/");
    }

    #[test]
    #[serial]
    fn builder_env_only_uses_network_config_ws() {
        let env_ctx = CliContextBuilder::new()
            .with_env(Environment::Devnet)
            .build()
            .unwrap();
        let cfg = Environment::Devnet.config().unwrap();
        assert_eq!(env_ctx.ledger_rpc_url, cfg.ledger_public_rpc_url);
        assert_eq!(env_ctx.ledger_ws_rpc_url, cfg.ledger_public_ws_rpc_url);
    }

    #[test]
    #[serial]
    fn builder_without_env_requires_all_fields() {
        let pk = solana_sdk::pubkey::Pubkey::new_unique();
        let ctx = CliContextBuilder::new()
            .with_ledger_rpc_url("https://custom-rpc.example/")
            .with_solana_l1_rpc_url("https://custom-l1.example/")
            .with_serviceability_program_id(pk)
            .with_geolocation_program_id(pk)
            .with_telemetry_program_id(pk)
            .build()
            .unwrap();
        assert_eq!(ctx.ledger_rpc_url, "https://custom-rpc.example/");
        assert_eq!(ctx.ledger_ws_rpc_url, "wss://custom-rpc.example/");
        assert_eq!(ctx.solana_l1_rpc_url, "https://custom-l1.example/");
    }

    #[test]
    #[serial]
    fn builder_without_env_fails_when_field_missing() {
        let err = CliContextBuilder::new()
            .with_ledger_rpc_url("https://custom-rpc.example/")
            .build()
            .unwrap_err();
        assert!(err.to_string().contains("solana_l1_rpc_url is required"));
    }
}
