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
    pub async fn try_into_execute(self) -> Result<()> {
        // Use the unlocked `Stdout` handle (Send across awaits); each write
        // locks internally, matching the pre-refactor `println!` behavior.
        let mut out = std::io::stdout();
        // The library verbs return a typed `PassportCliError`; `?` lifts it into
        // `anyhow::Error` via the blanket `From<E: Error>` impl, preserving the
        // cause chain (no `"{e:#}"` flattening).
        match self {
            PassportSubcommand::Fetch(FetchAdapter {
                inner,
                conn,
                json,
                json_compact,
            }) => {
                let ctx = build_ctx(conn, None, json, json_compact)?;
                inner.execute(&ctx, &mut out).await?;
            }
            PassportSubcommand::FindValidator(FindValidatorAdapter {
                inner,
                conn,
                json,
                json_compact,
            }) => {
                let ctx = build_ctx(conn, None, json, json_compact)?;
                inner.execute(&ctx, &mut out).await?;
            }
            PassportSubcommand::PrepareValidatorAccess(PrepareAdapter { inner, conn }) => {
                let ctx = build_ctx(conn, None, false, false)?;
                inner.execute(&ctx, &mut out).await?;
            }
            PassportSubcommand::RequestValidatorAccess(RequestAdapter {
                inner,
                conn,
                keypair_path,
            }) => {
                let ctx = build_ctx(conn, keypair_path, false, false)?;
                inner.execute(&ctx, &mut out).await?;
            }
        }
        Ok(())
    }
}

/// Build a `CliContext` from the legacy per-verb connection options.
///
/// The Solana L1 RPC URL is resolved exactly as the pre-RFC-20 passport verbs
/// did (`SolanaConnection::from(SolanaConnectionOptions)`), so monikers and raw
/// URLs behave identically. `env` is derived from the moniker (raw URLs default
/// to mainnet-beta for env-derived defaults, but the explicit L1 URL always
/// wins).
fn build_ctx(
    conn: SolanaConnectionOptions,
    keypair_path: Option<String>,
    json: bool,
    json_compact: bool,
) -> Result<CliContext> {
    let env = try_map_env(conn.moniker_env())?;
    let solana_l1_rpc_url = SolanaConnection::from(conn).url();

    let mut builder = CliContextBuilder::new()
        .with_env(env)
        .with_solana_l1_rpc_url(solana_l1_rpc_url)
        .with_output_format(OutputFormat::from_flags(json, json_compact));
    if let Some(path) = keypair_path {
        builder = builder.with_keypair_path(PathBuf::from(path));
    }
    builder.build().map_err(|e| anyhow::anyhow!("{e:#}"))
}

fn try_map_env(n: Option<NetworkEnvironment>) -> Result<Environment> {
    let env = match n.unwrap_or(NetworkEnvironment::MainnetBeta) {
        NetworkEnvironment::MainnetBeta => Environment::MainnetBeta,
        NetworkEnvironment::Testnet => Environment::Testnet,
        NetworkEnvironment::Devnet => anyhow::bail!("passport is not available on Solana devnet"),
        NetworkEnvironment::Localnet => Environment::Local,
    };
    Ok(env)
}
