mod distribute_rewards;
mod finalize_distribution_rewards;
mod sweep_distribution_tokens;

//

use anyhow::Result;
use chrono::Utc;
use clap::{Args, Subcommand, ValueEnum};
use doublezero_scheduled_command::Schedulable;
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, Wallet},
    rpc::DoubleZeroLedgerConnection,
};
use doublezero_solana_validator_debt::worker;
use slack_notifier::validator_debt;

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

impl RevenueDistributionRelaySubcommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::PaySolanaValidatorDebt {
                dz_epoch,
                solana_payer_options,
                export,
            } => execute_pay_solana_validator_debt(dz_epoch, solana_payer_options, export).await,
            Self::SweepDistributionTokens(command) => command.execute().await,
            Self::FinalizeDistributionRewards(command) => command.execute().await,
            Self::DistributeRewards(command) => command.execute().await,
        }
    }
}

async fn execute_pay_solana_validator_debt(
    epoch: u64,
    solana_payer_options: SolanaPayerOptions,
    export: Option<ExportFormat>,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;

    let dz_env = wallet.connection.try_dz_environment().await?;
    let dz_connection = DoubleZeroLedgerConnection::from(dz_env);

    let dry_run = wallet.dry_run;
    let tx_results = worker::pay_solana_validator_debt(wallet, dz_connection, epoch).await?;

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

        for tx_result in tx_results.collection_results {
            writer.serialize(tx_result)?;
        }
        filename = Some(string_filename);
        writer.flush()?;
    };
    if let Some(ExportFormat::Slack) = export {
        validator_debt::post_debt_collection_to_slack(
            tx_results.total_transactions_attempted,
            tx_results.successful_transactions,
            tx_results.insufficient_funds,
            tx_results.already_paid,
            epoch,
            filename,
            dry_run,
        )
        .await?;
    }

    Ok(())
}
