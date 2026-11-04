mod finalize_distribution_rewards;

// The relay verbs keep their pre-RFC-20 `SolanaPayerOptions` because the
// `Schedulable` trait's `execute_once(&self)` cannot take a `CliContext`;
// `patch_payer_opts` merges only the global `--solana-url`/`--keypair` into
// them. The global `--env` and `--dz-ledger-url` are NOT applied here — each
// relay verb resolves its DZ environment from its own (hidden) `--dz-env`
// flag or genesis-hash detection. Tracked for the #1520 migration.

use std::io::Write;

use anyhow::Result;
use clap::{Args, Subcommand};
use doublezero_cli_core::CliContext;
use doublezero_scheduled_command::Schedulable;
use doublezero_solana_client_tools::payer::SolanaPayerOptions;

#[derive(Debug, Args)]
pub struct RevenueDistributionRelayCommand {
    #[command(subcommand)]
    pub inner: RevenueDistributionRelaySubcommand,
}

#[derive(Debug, Subcommand)]
pub enum RevenueDistributionRelaySubcommand {
    FinalizeDistributionRewards(finalize_distribution_rewards::FinalizeDistributionRewards),
}

/// Inject `CliContext` defaults into a `SolanaPayerOptions` that may have been
/// left empty by the user (relay verbs keep `SolanaPayerOptions` because the
/// `Schedulable` trait requires `execute_once(&self)` — see #1520 for full
/// migration to `CliContext`).
fn patch_payer_opts(ctx: &CliContext, opts: &mut SolanaPayerOptions) {
    if opts.connection_options.solana_url_or_moniker.is_none() {
        opts.connection_options.solana_url_or_moniker = Some(ctx.solana_l1_rpc_url.clone());
    }
    if opts.signer_options.keypair_path.is_none() {
        opts.signer_options.keypair_path =
            ctx.keypair_path.as_ref().map(|p| p.display().to_string());
    }
}

impl RevenueDistributionRelaySubcommand {
    pub async fn execute(self, ctx: &CliContext, _out: &mut impl Write) -> Result<()> {
        match self {
            Self::FinalizeDistributionRewards(mut command) => {
                patch_payer_opts(ctx, &mut command.solana_payer_options);
                command.execute().await
            }
        }
    }
}
