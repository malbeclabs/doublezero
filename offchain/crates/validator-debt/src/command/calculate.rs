use anyhow::Result;
use chrono::Utc;
use clap::{Args, ValueEnum};
use doublezero_revenue_distribution::state::ProgramConfig;
use doublezero_scheduled_command::{Schedulable, ScheduleOption};
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, try_load_keypair},
    rpc::{DoubleZeroLedgerConnectionOptions, SolanaConnection, SolanaConnectionOptions},
};
use leaky_bucket::RateLimiter;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::commitment_config::CommitmentConfig;
use tabled::{Table, settings::Style};

use crate::{
    rpc::{JoinedSolanaEpochs, SolanaValidatorDebtConnectionOptions},
    solana_debt_calculator::SolanaDebtCalculator,
    transaction::Transaction,
};

#[derive(Debug, Clone, ValueEnum)]
pub enum ExportFormat {
    Csv,
    Slack,
}

#[derive(Debug, Args, Clone)]
pub struct CalculateValidatorDebtCommand {
    #[arg(long)]
    epoch: Option<u64>,

    #[command(flatten)]
    schedule_or_force: super::ScheduleOrForce,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,

    #[command(flatten)]
    dz_ledger_connection_options: DoubleZeroLedgerConnectionOptions,

    /// Option to post validator debt only to the DoubleZero Ledger
    #[arg(long)]
    post_to_ledger_only: bool,

    /// export results: csv, slack
    #[arg(long, value_enum)]
    export: Option<ExportFormat>,
}

#[async_trait::async_trait]
impl Schedulable for CalculateValidatorDebtCommand {
    fn schedule(&self) -> &ScheduleOption {
        &self.schedule_or_force.schedule
    }

    async fn execute_once(&self) -> Result<()> {
        let Self {
            epoch,
            schedule_or_force,
            solana_payer_options,
            dz_ledger_connection_options,
            post_to_ledger_only,
            export,
        } = self;

        schedule_or_force.ensure_safe_execution()?;

        let epoch = match epoch {
            Some(e) => *e,
            None => {
                latest_distribution_epoch(
                    &solana_payer_options.connection_options,
                    dz_ledger_connection_options,
                )
                .await?
            }
        };

        let connection_options = SolanaValidatorDebtConnectionOptions {
            solana_url_or_moniker: solana_payer_options
                .connection_options
                .solana_url_or_moniker
                .clone(),
            dz_ledger_url: dz_ledger_connection_options.dz_ledger_url.clone(),
        };
        let solana_debt_calculator: SolanaDebtCalculator =
            SolanaDebtCalculator::try_from(connection_options)?;
        let signer = try_load_keypair(None).expect("failed to load keypair");
        let transaction = Transaction::new(
            signer,
            solana_payer_options.signer_options.dry_run,
            schedule_or_force.force,
        );
        let dry_run = transaction.dry_run;
        let write_summary = crate::worker::calculate_distribution(
            &solana_debt_calculator,
            transaction,
            epoch,
            *post_to_ledger_only,
        )
        .await?;

        let mut filename: Option<String> = None;

        if let Some(ExportFormat::Csv) = export {
            let now = Utc::now();
            let timestamp_milliseconds: i64 = now.timestamp_millis();
            let string_filename = if dry_run {
                format!(
                    "DRY_RUN_dz_epoch_{}_calculate_distribution_{timestamp_milliseconds}.csv",
                    write_summary.dz_epoch
                )
            } else {
                format!(
                    "dz_epoch_{}_calculate_distribution_{timestamp_milliseconds}.csv",
                    write_summary.dz_epoch
                )
            };
            let mut writer = csv::Writer::from_path(string_filename.clone())?;
            filename = Some(string_filename);
            for w in write_summary.validator_summaries.iter() {
                writer.serialize(w)?;
            }
            writer.flush()?;
        };

        if let Some(ExportFormat::Slack) = export {
            slack_notifier::validator_debt::post_distribution_to_slack(
                filename,
                write_summary.solana_epoch,
                write_summary.dz_epoch,
                dry_run,
                write_summary.total_debt,
                write_summary.total_validators,
                write_summary.transaction_id,
            )
            .await?;
        }

        println!(
            "Validator rewards for solana epoch {} and validator debt for DoubleZero epoch {}:\n{}",
            write_summary.solana_epoch,
            write_summary.dz_epoch,
            Table::new(write_summary.validator_summaries).with(Style::psql().remove_horizontals())
        );

        Ok(())
    }
}

#[derive(Debug, Args, Clone)]
pub struct FindSolanaEpochCommand {
    /// Target DoubleZero Ledger epoch.
    #[arg(long)]
    epoch: Option<u64>,

    #[command(flatten)]
    schedule_or_force: super::ScheduleOrForce,

    #[command(flatten)]
    solana_connection_options: SolanaConnectionOptions,

    #[command(flatten)]
    dz_ledger_connection_options: DoubleZeroLedgerConnectionOptions,

    /// Limit requests per second for Solana RPC.
    #[arg(long, default_value_t = 10)]
    solana_rate_limit: usize,
}

#[async_trait::async_trait]
impl Schedulable for FindSolanaEpochCommand {
    fn schedule(&self) -> &ScheduleOption {
        &self.schedule_or_force.schedule
    }

    async fn execute_once(&self) -> Result<()> {
        let Self {
            epoch,
            schedule_or_force,
            solana_connection_options,
            dz_ledger_connection_options,
            solana_rate_limit,
        } = self;

        schedule_or_force.ensure_safe_execution()?;

        let latest_distribution_epoch =
            latest_distribution_epoch(solana_connection_options, dz_ledger_connection_options)
                .await?;

        let target_dz_epoch = epoch.as_ref().copied().unwrap_or(latest_distribution_epoch);
        tracing::info!("Target DZ epoch: {target_dz_epoch}");

        let rate_limiter = RateLimiter::builder()
            .max(*solana_rate_limit)
            .initial(*solana_rate_limit)
            .refill(*solana_rate_limit)
            .interval(std::time::Duration::from_secs(1))
            .build();

        let solana_connection = SolanaConnection::from(solana_connection_options.clone());

        let dz_ledger_rpc_client = RpcClient::new_with_commitment(
            dz_ledger_connection_options.dz_ledger_url.clone(),
            CommitmentConfig::confirmed(),
        );

        match JoinedSolanaEpochs::try_new(
            &solana_connection,
            &dz_ledger_rpc_client,
            target_dz_epoch,
            &rate_limiter,
        )
        .await?
        {
            JoinedSolanaEpochs::Range(solana_epoch_range) => {
                solana_epoch_range.into_iter().for_each(|solana_epoch| {
                    tracing::info!("Joined Solana epoch: {solana_epoch}");
                });
            }
            JoinedSolanaEpochs::Duplicate(solana_epoch) => {
                tracing::warn!("Duplicated joined Solana epoch: {solana_epoch}");
            }
        };

        Ok(())
    }
}

// TODO: Does the dz ledger connection need to be an argument? Also, this is a
// duplicate of the function in verify.rs.
async fn latest_distribution_epoch(
    solana_connection_options: &SolanaConnectionOptions,
    dz_ledger_connection_options: &DoubleZeroLedgerConnectionOptions,
) -> Result<u64> {
    let solana_connection = SolanaConnection::from(solana_connection_options.clone());
    let is_mainnet = solana_connection.try_is_mainnet().await?;

    let dz_ledger_rpc_client = RpcClient::new_with_commitment(
        dz_ledger_connection_options.dz_ledger_url.clone(),
        CommitmentConfig::confirmed(),
    );

    super::ensure_same_network_environment(&dz_ledger_rpc_client, is_mainnet).await?;

    let program_config = solana_connection
        .try_fetch_zero_copy_data::<ProgramConfig>(&ProgramConfig::find_address().0)
        .await?;

    Ok(program_config
        .next_completed_dz_epoch
        .value()
        .saturating_sub(1))
}
