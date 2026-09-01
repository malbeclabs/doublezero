mod passport;
mod revenue_distribution;
mod shreds;

//

use std::io::Write;

use anyhow::Result;
use clap::{Args, Subcommand};
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, SolanaSignerOptions, Wallet},
    rpc::{NetworkEnvironment, SolanaConnection, SolanaConnectionOptions},
};

// ── Shared types & helpers ──────────────────────────────────────────────

/// Per-verb options for write verbs: the signer-specific flags plus per-verb
/// `--url`/`--keypair` overrides (back-compat) that win over the global
/// `CliContext` values when present.
#[derive(Debug, Args, Clone, Default)]
pub struct WriteVerbOptions {
    /// Solana RPC URL or moniker. Overrides the global value for this verb.
    #[command(flatten)]
    pub connection_options: SolanaConnectionOptions,

    /// Filepath or URL to a keypair. Overrides the global keypair for this verb.
    #[arg(long = "keypair", short = 'k', value_name = "KEYPAIR", env)]
    pub keypair_path: Option<String>,

    /// Set the compute unit price (micro-lamports per compute unit).
    #[arg(long, value_name = "MICROLAMPORTS", env)]
    pub with_compute_unit_price: Option<u64>,

    /// Print verbose output.
    #[arg(long, short = 'v', default_value = "false", env)]
    pub verbose: bool,

    /// Filepath or URL to keypair to pay transaction fee.
    #[arg(long = "fee-payer", value_name = "KEYPAIR", env)]
    pub fee_payer_path: Option<String>,

    /// Simulate transaction only.
    #[arg(long, env)]
    pub dry_run: bool,
}

/// Build a [`Wallet`] from the global `CliContext` and per-verb write options.
/// Per-verb `--url`/`--keypair` (back-compat) win over the global CliContext.
pub(crate) fn build_wallet(ctx: &CliContext, opts: WriteVerbOptions) -> Result<Wallet> {
    let solana_url_or_moniker = opts
        .connection_options
        .solana_url_or_moniker
        .or_else(|| Some(ctx.solana_l1_rpc_url.clone()));
    let keypair_path = opts
        .keypair_path
        .or_else(|| ctx.keypair_path.as_ref().map(|p| p.display().to_string()));
    let payer_opts = SolanaPayerOptions {
        connection_options: SolanaConnectionOptions {
            solana_url_or_moniker,
        },
        signer_options: SolanaSignerOptions {
            keypair_path,
            with_compute_unit_price: opts.with_compute_unit_price,
            verbose: opts.verbose,
            fee_payer_path: opts.fee_payer_path,
            dry_run: opts.dry_run,
        },
    };
    Wallet::try_new(payer_opts, None)
}

/// Build a `SolanaConnection`: per-verb `--url` (back-compat) wins over the
/// global ctx L1 URL.
pub(crate) fn solana_connection(
    ctx: &CliContext,
    connection_options: &SolanaConnectionOptions,
) -> SolanaConnection {
    if connection_options.solana_url_or_moniker.is_some() {
        SolanaConnection::from(connection_options.clone())
    } else {
        SolanaConnection::new(ctx.solana_l1_rpc_url.clone())
    }
}

/// Resolve the `NetworkEnvironment` the way the pre-RFC-20 CLI did: a per-verb
/// `-u <moniker>` wins; otherwise detect from the connection's genesis hash.
/// The global `--env`/`--solana-url` flags participate through the URL the
/// connection was built from, so they supply the default without changing the
/// behavior of invocations that predate them.
pub(crate) async fn resolve_network_env(
    connection: &SolanaConnection,
    moniker_env: Option<NetworkEnvironment>,
) -> Result<NetworkEnvironment> {
    match moniker_env {
        Some(environment) => Ok(environment),
        None => connection.try_network_environment().await,
    }
}

// ── Top-level dispatch ──────────────────────────────────────────────────

#[derive(Debug, Subcommand)]
pub enum DoubleZeroSolanaCommand {
    /// Passport program commands.
    Passport(passport::PassportCommand),

    /// Revenue distribution program commands.
    RevenueDistribution(revenue_distribution::RevenueDistributionCommand),

    /// Shred subscription program commands.
    Shreds(shreds::ShredsCommand),
}

impl DoubleZeroSolanaCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        match self {
            Self::Passport(passport) => passport.command.execute(ctx, out).await,
            Self::RevenueDistribution(revenue_distribution) => {
                revenue_distribution.command.execute(ctx, out).await
            }
            Self::Shreds(shreds) => shreds.execute(ctx, out).await,
        }
    }
}

// ── Shared interactive prompt ───────────────────────────────────────────

pub(crate) fn try_prompt_proceed_confirmation(
    out: &mut impl Write,
    prompt_message: &str,
    abort_message: &str,
) -> Result<()> {
    loop {
        writeln!(out, "⚠️  {prompt_message}. Proceed? [y/N]")?;
        // A buffered writer would otherwise hold the prompt while stdin blocks.
        out.flush()?;

        let mut input = String::new();
        std::io::stdin().read_line(&mut input)?;

        let first_char = input
            .trim()
            .chars()
            .next()
            .map(|c| c.to_lowercase().next().unwrap());

        match first_char {
            Some('y') => return Ok(()),
            Some('n') | None => anyhow::bail!("{abort_message}"),
            _ => {
                writeln!(
                    out,
                    "Invalid input. Please enter 'y' for yes or 'n' for no."
                )?;
                continue;
            }
        }
    }
}
