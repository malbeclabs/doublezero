//! Backward-compatibility adapter for the `passport` command tree.
//!
//! The passport verbs now live in the RFC-20 module crate
//! `doublezero-passport-cli`, whose verbs read all connection/identity
//! configuration from a `CliContext`. This adapter preserves the exact
//! pre-RFC-20 `doublezero-solana passport ...` flag surface (per-verb `--url`,
//! `--keypair`, signer flags) by re-declaring those flags here, building a
//! `CliContext` from them, and delegating to the library's `execute(ctx, out)`.
//!
//! New flags (`--json`, `--json-compact`) are additive on the read verbs.

use std::path::PathBuf;

use anyhow::Result;
use clap::{Args, Subcommand};
use doublezero_cli_core::{CliContext, CliContextBuilder, OutputFormat};
use doublezero_config::Environment;
use doublezero_passport_cli as lib;
use doublezero_solana_client_tools::rpc::{
    NetworkEnvironment, SolanaConnection, SolanaConnectionOptions,
};

#[derive(Debug, Args)]
pub struct PassportCommand {
    #[command(subcommand)]
    pub command: PassportSubcommand,
}

#[derive(Debug, Subcommand)]
pub enum PassportSubcommand {
    /// Fetch and display the current program configuration and access request (if any)
    Fetch(FetchAdapter),
    /// Find and display the Current Identity
    FindValidator(FindValidatorAdapter),
    /// Validate arguments and generate the required transaction signature command
    PrepareValidatorAccess(PrepareAdapter),
    /// Request access as a Solana Validator
    RequestValidatorAccess(RequestAdapter),
}

#[derive(Debug, Args)]
pub struct FetchAdapter {
    #[command(flatten)]
    inner: lib::fetch::FetchArgs,
    #[command(flatten)]
    conn: SolanaConnectionOptions,
    /// Output as pretty JSON
    #[arg(long, default_value_t = false, conflicts_with = "json_compact")]
    json: bool,
    /// Output as single-line JSON suitable for piping
    #[arg(
        long = "json-compact",
        default_value_t = false,
        conflicts_with = "json"
    )]
    json_compact: bool,
}

#[derive(Debug, Args)]
pub struct FindValidatorAdapter {
    #[command(flatten)]
    inner: lib::find_validator::FindValidatorArgs,
    #[command(flatten)]
    conn: SolanaConnectionOptions,
    /// Output as pretty JSON
    #[arg(long, default_value_t = false, conflicts_with = "json_compact")]
    json: bool,
    /// Output as single-line JSON suitable for piping
    #[arg(
        long = "json-compact",
        default_value_t = false,
        conflicts_with = "json"
    )]
    json_compact: bool,
}

#[derive(Debug, Args)]
pub struct PrepareAdapter {
    #[command(flatten)]
    inner: lib::prepare_access::PrepareValidatorAccessArgs,
    #[command(flatten)]
    conn: SolanaConnectionOptions,
}

#[derive(Debug, Args)]
pub struct RequestAdapter {
    #[command(flatten)]
    inner: lib::request_access::RequestValidatorAccessArgs,
    #[command(flatten)]
    conn: SolanaConnectionOptions,
    /// Filepath or URL to a keypair.
    #[arg(long = "keypair", short = 'k', value_name = "KEYPAIR", env)]
    keypair_path: Option<String>,
}

impl PassportSubcommand {
    pub async fn execute(
        self,
        parent_ctx: &CliContext,
        out: &mut impl std::io::Write,
    ) -> Result<()> {
        match self {
            PassportSubcommand::Fetch(FetchAdapter {
                inner,
                conn,
                json,
                json_compact,
            }) => {
                let ctx = merge_ctx(parent_ctx, conn, None, json, json_compact)?;
                inner.execute(&ctx, out).await?;
            }
            PassportSubcommand::FindValidator(FindValidatorAdapter {
                inner,
                conn,
                json,
                json_compact,
            }) => {
                let ctx = merge_ctx(parent_ctx, conn, None, json, json_compact)?;
                inner.execute(&ctx, out).await?;
            }
            PassportSubcommand::PrepareValidatorAccess(PrepareAdapter { inner, conn }) => {
                let ctx = merge_ctx(parent_ctx, conn, None, false, false)?;
                inner.execute(&ctx, out).await?;
            }
            PassportSubcommand::RequestValidatorAccess(RequestAdapter {
                inner,
                conn,
                keypair_path,
            }) => {
                let ctx = merge_ctx(parent_ctx, conn, keypair_path, false, false)?;
                inner.execute(&ctx, out).await?;
            }
        }
        Ok(())
    }
}

/// Build a `CliContext` by merging the global context with per-verb overrides.
///
/// Per-verb `--url` / `--keypair` / `--json` flags (kept for backward
/// compatibility) take precedence over the global context. When the per-verb
/// flags are absent, the global context values are used.
fn merge_ctx(
    parent: &CliContext,
    conn: SolanaConnectionOptions,
    keypair_path: Option<String>,
    json: bool,
    json_compact: bool,
) -> Result<CliContext> {
    // Per-verb moniker (back-compat) wins and is validated (passport is not
    // deployed on Solana devnet); otherwise inherit the global ctx env. When
    // the moniker overrides the env, the parent's ledger URL is NOT pinned —
    // the builder derives it from the overridden env so the resolved context
    // stays internally consistent (env, ledger, and program IDs agree).
    let (env, ledger_rpc_url) = match conn.moniker_env() {
        Some(moniker_env) => (try_map_env(moniker_env)?, None),
        None => (parent.env, Some(parent.ledger_rpc_url.clone())),
    };
    let solana_l1_rpc_url = if conn.solana_url_or_moniker.is_some() {
        SolanaConnection::from(conn).url()
    } else {
        parent.solana_l1_rpc_url.clone()
    };
    let output_format = if json || json_compact {
        OutputFormat::from_flags(json, json_compact)
    } else {
        parent.output_format
    };
    let keypair = keypair_path
        .map(PathBuf::from)
        .or_else(|| parent.keypair_path.clone());

    // Maintenance hazard: this rebuilds a `CliContext` field-by-field rather
    // than overriding the parent, so any field added to `CliContext` is
    // silently dropped here until mirrored below. Keep in sync, or replace with
    // a `CliContext::with_overrides()` helper in cli-core if one lands.
    let mut builder = CliContextBuilder::new()
        .with_env(env)
        .with_solana_l1_rpc_url(solana_l1_rpc_url)
        .with_output_format(output_format)
        .with_client_version(parent.client_version.clone());
    if let Some(url) = ledger_rpc_url {
        builder = builder.with_ledger_rpc_url(url);
    }
    if let Some(path) = keypair {
        builder = builder.with_keypair_path(path);
    }
    builder.build().map_err(anyhow::Error::msg)
}

fn try_map_env(network_environment: NetworkEnvironment) -> Result<Environment> {
    let env = match network_environment {
        NetworkEnvironment::MainnetBeta => Environment::MainnetBeta,
        NetworkEnvironment::Testnet => Environment::Testnet,
        NetworkEnvironment::Devnet => anyhow::bail!("passport is not available on Solana devnet"),
        NetworkEnvironment::Localnet => Environment::Local,
    };
    Ok(env)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parent_ctx() -> CliContext {
        CliContextBuilder::new()
            .with_env(Environment::MainnetBeta)
            .with_keypair_path(PathBuf::from("/tmp/parent-id.json"))
            .with_client_version("1.2.3")
            .build()
            .expect("parent ctx builds")
    }

    /// A trailing `-u <moniker>` overrides the env AND every env-derived field
    /// follows it: the ledger URL is re-derived from the overridden env rather
    /// than pinned to the parent's, so the merged context stays internally
    /// consistent. Also locks in that non-overridden fields (keypair,
    /// client_version) survive the rebuild.
    #[test]
    fn test_merge_ctx_moniker_override_rederives_env_fields() {
        let parent = parent_ctx();
        let conn = SolanaConnectionOptions {
            solana_url_or_moniker: Some("t".to_string()),
        };
        let merged = merge_ctx(&parent, conn, None, false, false).expect("merge succeeds");

        let testnet = Environment::Testnet.config().expect("testnet config");
        assert_eq!(merged.env, Environment::Testnet);
        assert_eq!(merged.ledger_rpc_url, testnet.ledger_public_rpc_url);
        assert_ne!(merged.ledger_rpc_url, parent.ledger_rpc_url);
        assert_eq!(merged.keypair_path, parent.keypair_path);
        assert_eq!(merged.client_version, parent.client_version);
    }

    /// Without per-verb overrides the parent context passes through unchanged.
    #[test]
    fn test_merge_ctx_without_overrides_inherits_parent() {
        let parent = parent_ctx();
        let merged = merge_ctx(
            &parent,
            SolanaConnectionOptions::default(),
            None,
            false,
            false,
        )
        .expect("merge succeeds");

        assert_eq!(merged.env, parent.env);
        assert_eq!(merged.solana_l1_rpc_url, parent.solana_l1_rpc_url);
        assert_eq!(merged.ledger_rpc_url, parent.ledger_rpc_url);
        assert_eq!(merged.keypair_path, parent.keypair_path);
        assert_eq!(merged.output_format, parent.output_format);
        assert_eq!(merged.client_version, parent.client_version);
    }

    /// The devnet moniker is rejected with an actionable error (passport is not
    /// deployed on the Solana devnet cluster).
    #[test]
    fn test_merge_ctx_devnet_moniker_errors() {
        let parent = parent_ctx();
        let conn = SolanaConnectionOptions {
            solana_url_or_moniker: Some("devnet".to_string()),
        };
        let err = merge_ctx(&parent, conn, None, false, false)
            .expect_err("devnet moniker must be rejected");
        assert!(err.to_string().contains("not available on Solana devnet"));
    }
}
