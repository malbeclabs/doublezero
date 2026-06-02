use anyhow::Result;
use clap::Parser;
use doublezero_solana_cli::command::DoubleZeroSolanaCommand;
use tracing_subscriber::{EnvFilter, layer::SubscriberExt, util::SubscriberInitExt};

#[derive(Debug, Parser)]
#[command(term_width = 0)]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero Solana-related Commands", long_about = None)]
struct DoubleZeroSolanaApp {
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

    DoubleZeroSolanaApp::parse()
        .command
        .try_into_execute()
        .await
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
    fn command_tree_is_valid() {
        DoubleZeroSolanaApp::command().debug_assert();
    }

    /// Legacy `passport fetch` invocation must still parse unchanged.
    #[test]
    fn passport_fetch_legacy_args_parse() {
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
    fn passport_fetch_accepts_json_flags() {
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
    fn passport_find_validator_legacy_and_json_parse() {
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
    fn passport_request_access_legacy_signer_flags_parse() {
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
    fn passport_prepare_access_legacy_args_parse() {
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
}
