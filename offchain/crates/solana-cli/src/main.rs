use std::path::PathBuf;

use anyhow::Result;
use clap::Parser;
use doublezero_cli_core::CliContextBuilder;
use doublezero_config::Environment;
use doublezero_solana_cli::command::DoubleZeroSolanaCommand;
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};
use tracing_subscriber::{EnvFilter, layer::SubscriberExt, util::SubscriberInitExt};

#[derive(Debug, Parser)]
#[command(term_width = 0)]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero Solana-related Commands", long_about = None)]
struct DoubleZeroSolanaApp {
    /// DoubleZero environment: mainnet-beta (default), testnet, devnet, local.
    /// This is the DoubleZero environment taxonomy (matching the `doublezero`
    /// CLI), not a Solana cluster name: `devnet` is the DZ devnet environment,
    /// whose Solana L1 is testnet. To target the Solana devnet cluster, pass
    /// `-u devnet` after the subcommand.
    #[arg(long, default_value_t = Environment::MainnetBeta)]
    env: Environment,

    /// Solana RPC URL or moniker. Overrides the environment default.
    #[arg(long = "solana-url", visible_alias = "url", short = 'u', env)]
    solana_url: Option<String>,

    /// DZ Ledger RPC URL override. When omitted, derived from --env. Consumed
    /// by the shreds subcommands (device-code resolution) and carried in the
    /// context for passport; the revenue-distribution verbs resolve the DZ
    /// Ledger from their own (hidden) --dz-env flag pending #1520.
    #[arg(long, env)]
    dz_ledger_url: Option<String>,

    /// Filepath or URL to a keypair.
    #[arg(long = "keypair", short = 'k', env)]
    keypair_path: Option<String>,

    #[command(subcommand)]
    command: DoubleZeroSolanaCommand,
}

#[tokio::main]
async fn main() -> Result<()> {
    unsafe {
        libc::signal(libc::SIGPIPE, libc::SIG_DFL);
    }

    tracing_subscriber::registry()
        .with(EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")))
        .with(
            tracing_subscriber::fmt::layer()
                .with_target(false)
                .with_thread_ids(false)
                .with_thread_names(false),
        )
        .init();

    let mut app = DoubleZeroSolanaApp::parse();
    // The shreds subcommands take `--dz-ledger-url` in the pre-RFC-20 position
    // (`shreds --dz-ledger-url <URL> <verb>`); the global flag fills that slot
    // when the subcommand-level one is absent, so both spellings work and the
    // subcommand-level one wins.
    if let DoubleZeroSolanaCommand::Shreds(ref mut shreds) = app.command
        && shreds.dz_ledger_url.is_none()
    {
        shreds.dz_ledger_url = app.dz_ledger_url.clone();
    }
    let ctx = build_cli_context(&app)?;
    let mut out = std::io::stdout();
    app.command.execute(&ctx, &mut out).await
}

fn build_cli_context(app: &DoubleZeroSolanaApp) -> Result<doublezero_cli_core::CliContext> {
    let mut builder = CliContextBuilder::new()
        .with_env(app.env)
        .with_client_version(option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")));
    // --solana-url may be a moniker; resolve it to a URL. When absent, the
    // builder derives the L1 URL (and every other unset field) from --env.
    if let Some(ref url_or_moniker) = app.solana_url {
        let url = SolanaConnection::from(SolanaConnectionOptions {
            solana_url_or_moniker: Some(url_or_moniker.clone()),
        })
        .url()
        .to_string();
        builder = builder.with_solana_l1_rpc_url(url);
    }
    if let Some(ref url) = app.dz_ledger_url {
        builder = builder.with_ledger_rpc_url(url.clone());
    }
    if let Some(ref path) = app.keypair_path {
        builder = builder.with_keypair_path(PathBuf::from(path));
    }
    builder.build().map_err(anyhow::Error::msg)
}

#[cfg(test)]
mod tests {
    use clap::CommandFactory;

    use super::*;

    const PK: &str = "DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb";
    const SIG: &str =
        "5wHu1qwD4kLwd9DnXcAgkbdJVDQfqQfXY3xn2pxBYNqDjT9rh9XkVxqGc8gQH6w2xR8jKfP4t1pYqJ7sJ5h4wK2";

    /// clap's own consistency checks for the whole command tree (no overlapping
    /// flags, valid arg config, etc.).
    #[test]
    fn test_command_tree_is_valid() {
        DoubleZeroSolanaApp::command().debug_assert();
    }

    /// `--env` selects a DoubleZero environment and every unset context field
    /// is derived from that environment's config: with `--env testnet`, the
    /// L1 URL, DZ-ledger URL, and serviceability program ID all come from the
    /// testnet `NetworkConfig`.
    #[test]
    fn test_build_cli_context_resolves_env_defaults_from_env_flag() {
        let app = DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "--env",
            "testnet",
            "passport",
            "fetch",
            "--config",
        ])
        .expect("--env testnet should parse");
        let ctx = build_cli_context(&app).expect("CliContext should build");

        let config = Environment::Testnet
            .config()
            .expect("testnet network config");
        assert_eq!(ctx.env, Environment::Testnet);
        assert_eq!(ctx.ledger_rpc_url, config.ledger_public_rpc_url);
        assert_eq!(
            ctx.serviceability_program_id,
            config.serviceability_program_id
        );
        // Guarded against an ambient `SOLANA_URL` env var that would populate
        // `--solana-url` and override the env default.
        if app.solana_url.is_none() {
            assert_eq!(ctx.solana_l1_rpc_url, config.solana_l1_rpc_url);
        }
    }

    /// `--env devnet` means the DoubleZero devnet environment (whose Solana L1
    /// is testnet), NOT the Solana devnet cluster — matching the `doublezero`
    /// CLI's `--env` taxonomy. The context is internally consistent: all fields
    /// come from the DZ devnet config. The Solana devnet cluster remains
    /// reachable with a trailing `-u devnet`.
    #[test]
    fn test_build_cli_context_devnet_is_doublezero_devnet() {
        let app = DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "--env",
            "devnet",
            "shreds",
            "price",
            "--device",
            PK,
        ])
        .expect("--env devnet should parse");
        let ctx = build_cli_context(&app).expect("CliContext should build");

        let config = Environment::Devnet.config().expect("devnet network config");
        assert_eq!(ctx.env, Environment::Devnet);
        assert_eq!(
            ctx.serviceability_program_id,
            config.serviceability_program_id
        );
        if app.solana_url.is_none() {
            assert_eq!(ctx.solana_l1_rpc_url, config.solana_l1_rpc_url);
        }
    }

    /// With no global flags at all, the context defaults match the pre-RFC-20
    /// CLI: mainnet-beta, public mainnet L1 URL.
    #[test]
    fn test_build_cli_context_defaults_to_mainnet() {
        let app = DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "revenue-distribution",
            "fetch",
            "config",
        ])
        .expect("bare invocation should parse");
        let ctx = build_cli_context(&app).expect("CliContext should build");

        assert_eq!(ctx.env, Environment::MainnetBeta);
        if app.solana_url.is_none() {
            assert_eq!(ctx.solana_l1_rpc_url, "https://api.mainnet-beta.solana.com");
        }
    }

    /// Global flags + passport fetch invocation parses.
    #[test]
    fn test_passport_fetch_with_global_flags_parse() {
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "--env",
            "testnet",
            "passport",
            "fetch",
            "--config",
        ])
        .expect("global --env + fetch should parse");
    }

    /// Legacy per-verb `--url` on passport fetch still parses (adapter keeps it).
    #[test]
    fn test_passport_fetch_legacy_args_parse() {
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "passport",
            "fetch",
            "--config",
            "--url",
            "t",
        ])
        .expect("legacy fetch args should parse");
    }

    /// The additive `--json` / `--json-compact` flags must parse on read verbs.
    #[test]
    fn test_passport_fetch_accepts_json_flags() {
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "passport",
            "fetch",
            "--config",
            "--json",
        ])
        .expect("--json should parse");
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "passport",
            "fetch",
            "--config",
            "--json-compact",
        ])
        .expect("--json-compact should parse");
        // --json and --json-compact are mutually exclusive.
        assert!(
            DoubleZeroSolanaApp::try_parse_from([
                "doublezero-solana",
                "passport",
                "fetch",
                "--json",
                "--json-compact",
            ])
            .is_err()
        );
    }

    #[test]
    fn test_passport_find_validator_legacy_and_json_parse() {
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "passport",
            "find-validator",
            "--validator-id",
            PK,
            "-u",
            "m",
        ])
        .expect("legacy find-validator args should parse");
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "passport",
            "find-validator",
            "--json",
        ])
        .expect("find-validator --json should parse");
    }

    #[test]
    fn test_passport_request_access_legacy_signer_flags_parse() {
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "passport",
            "request-validator-access",
            "--doublezero-address",
            PK,
            "--primary-validator-id",
            PK,
            "--signature",
            SIG,
            "-k",
            "/path/to/id.json",
            "--with-compute-unit-price",
            "1000",
            "-v",
            "--dry-run",
            "--message-version",
            "0",
        ])
        .expect("legacy request-validator-access signer flags should parse");
    }

    #[test]
    fn test_passport_prepare_access_legacy_args_parse() {
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "passport",
            "prepare-validator-access",
            "--doublezero-address",
            PK,
            "--primary-validator-id",
            PK,
            "--backup-validator-ids",
            &format!("{PK},{PK}"),
            "--url",
            "t",
        ])
        .expect("legacy prepare-validator-access args should parse");
    }

    /// Global `--solana-url` alias `--url` parses at the app level.
    #[test]
    fn test_global_url_alias_parses() {
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "--url",
            "t",
            "revenue-distribution",
            "fetch",
            "config",
        ])
        .expect("global --url alias should parse");
    }

    /// Global `--keypair` parses at the app level.
    #[test]
    fn test_global_keypair_parses() {
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "-k",
            "/path/to/id.json",
            "revenue-distribution",
            "fetch",
            "config",
        ])
        .expect("global -k should parse");
    }

    // ── Backwards-compat: per-verb flags AFTER the subcommand ───────────────
    //
    // Pre-RFC-20 scripts pass `-u`/`-k`/`--dz-env` trailing the verb. These
    // must keep parsing (they override the new global flags for that verb).

    /// Trailing `-u` on a revenue-distribution read verb still parses.
    #[test]
    fn test_trailing_url_flag_still_parses() {
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "revenue-distribution",
            "fetch",
            "config",
            "-ul",
        ])
        .expect("trailing -u on `fetch config` should parse");
    }

    /// Trailing `-u` plus the hidden `--dz-env` on a read verb still parses.
    #[test]
    fn test_trailing_url_and_dz_env_flags_still_parse() {
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "revenue-distribution",
            "fetch",
            "distribution",
            "-ul",
            "--dz-env",
            "mainnet-beta",
        ])
        .expect("trailing -u + --dz-env on `fetch distribution` should parse");
    }

    /// Trailing `-k` on a revenue-distribution write verb still parses.
    #[test]
    fn test_trailing_keypair_flag_still_parses() {
        DoubleZeroSolanaApp::try_parse_from([
            "doublezero-solana",
            "revenue-distribution",
            "configure-contributor-rewards",
            "--service-key",
            PK,
            "-k",
            "/tmp/x.json",
        ])
        .expect("trailing -k on `configure-contributor-rewards` should parse");
    }

    /// Trailing `-u` on a shreds read verb still parses.
    #[test]
    fn test_trailing_url_flag_on_shreds_read_verb_still_parses() {
        DoubleZeroSolanaApp::try_parse_from(["doublezero-solana", "shreds", "price", "-ul"])
            .expect("trailing -u on `shreds price` should parse");
    }
}
