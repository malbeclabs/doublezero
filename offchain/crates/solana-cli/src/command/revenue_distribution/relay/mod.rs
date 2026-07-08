mod distribute_rewards;
mod finalize_distribution_rewards;
mod sweep_distribution_tokens;

// The relay verbs keep their pre-RFC-20 `SolanaPayerOptions` because the
// `Schedulable` trait's `execute_once(&self)` cannot take a `CliContext`;
// `patch_payer_opts` merges only the global `--solana-url`/`--keypair` into
// them. The global `--env` and `--dz-ledger-url` are NOT applied here — each
// relay verb resolves its DZ environment from its own (hidden) `--dz-env`
// flag or genesis-hash detection. Tracked for the #1520 migration.

use std::io::Write;

use anyhow::Result;
use chrono::Utc;
use clap::{Args, Subcommand, ValueEnum};
use doublezero_cli_core::CliContext;
use doublezero_scheduled_command::Schedulable;
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, Wallet},
    rpc::DoubleZeroLedgerConnection,
};
use doublezero_solana_sdk::revenue_distribution::fetch::{
    try_fetch_config, try_fetch_distribution,
};
use doublezero_solana_validator_debt::worker;

#[derive(Debug, Clone, ValueEnum)]
pub enum ExportFormat {
    Csv,
    Slack,
}

#[derive(Debug, Args)]
pub struct RevenueDistributionRelayCommand {
    #[command(subcommand)]
    pub inner: RevenueDistributionRelaySubcommand,
}

#[derive(Debug, Subcommand)]
pub enum RevenueDistributionRelaySubcommand {
    // TODO: add schedule
    PaySolanaValidatorDebt {
        #[arg(long)]
        dz_epoch: u64,

        /// export results: csv, slack
        #[arg(long, value_enum)]
        export: Option<ExportFormat>,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    SweepDistributionTokens(sweep_distribution_tokens::SweepDistributionTokens),

    FinalizeDistributionRewards(finalize_distribution_rewards::FinalizeDistributionRewards),

    DistributeRewards(distribute_rewards::DistributeRewards),
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
            Self::PaySolanaValidatorDebt {
                dz_epoch,
                mut solana_payer_options,
                export,
            } => {
                patch_payer_opts(ctx, &mut solana_payer_options);
                execute_pay_solana_validator_debt(dz_epoch, solana_payer_options, export).await
            }
            Self::SweepDistributionTokens(mut command) => {
                patch_payer_opts(ctx, &mut command.solana_payer_options);
                command.execute().await
            }
            Self::FinalizeDistributionRewards(mut command) => {
                patch_payer_opts(ctx, &mut command.solana_payer_options);
                command.execute().await
            }
            Self::DistributeRewards(mut command) => {
                patch_payer_opts(ctx, &mut command.solana_payer_options);
                command.execute().await
            }
        }
    }
}

async fn execute_pay_solana_validator_debt(
    epoch: u64,
    solana_payer_options: SolanaPayerOptions,
    export: Option<ExportFormat>,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;

    let dz_env = wallet.connection.try_network_environment().await?;
    let dz_connection = DoubleZeroLedgerConnection::from(dz_env);

    let dry_run = wallet.dry_run;
    let (_, config) = try_fetch_config(&wallet.connection).await?;

    let (_, distribution) = try_fetch_distribution(&wallet.connection, epoch).await?;

    if !distribution.is_debt_calculation_finalized() {
        tracing::warn!("{epoch} is not finalized, skipping");
        return Ok(());
    }

    let tx_results =
        worker::pay_solana_validator_debt(&wallet, &dz_connection, epoch, &config, &distribution)
            .await?;

    let mut filename: Option<String> = None;

    if let Some(ExportFormat::Csv) = export {
        let now = Utc::now();
        let timestamp_milliseconds: i64 = now.timestamp_millis();
        let string_filename = if dry_run {
            format!("DRY_RUN_dz_epoch_{epoch}_pay_solana_debt_{timestamp_milliseconds}.csv")
        } else {
            format!("dz_epoch_{epoch}_pay_solana_debt_{timestamp_milliseconds}.csv")
        };
        let mut writer = csv::Writer::from_path(string_filename.clone())?;

        for tx_result in tx_results.collection_results.clone() {
            writer.serialize(tx_result)?;
        }
        filename = Some(string_filename);
        writer.flush()?;
    };

    if let Some(ExportFormat::Slack) = export {
        worker::post_debt_collection_to_slack(tx_results, dry_run, filename).await?;
    }

    Ok(())
}
