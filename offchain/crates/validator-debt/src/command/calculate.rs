use anyhow::{Result, anyhow};
use clap::Args;
use doublezero_revenue_distribution::state::ProgramConfig;
use doublezero_scheduled_command::{Schedulable, ScheduleOption};
use doublezero_solana_client_tools::{
    log_info, log_warn,
    payer::{SolanaPayerOptions, try_load_keypair},
    rpc::{DoubleZeroLedgerConnectionOptions, SolanaConnection, SolanaConnectionOptions},
    zero_copy::ZeroCopyAccountOwned,
};
use leaky_bucket::RateLimiter;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::commitment_config::CommitmentConfig;

use crate::{
    rpc::{JoinedSolanaEpochs, SolanaValidatorDebtConnectionOptions},
    solana_debt_calculator::SolanaDebtCalculator,
    transaction::Transaction,
};

#[derive(Debug, Args, Clone)]
pub struct CalculateValidatorDebtCommand {
    #[arg(long)]
    epoch: u64,

    #[command(flatten)]
    schedule_or_force: super::ScheduleOrForce,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,

    #[command(flatten)]
    dz_ledger_connection_options: DoubleZeroLedgerConnectionOptions,

    /// Option to post validator debt only to the DoubleZero Ledger
    #[arg(long)]
    post_to_ledger_only: bool,
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
        } = self;

        schedule_or_force.ensure_safe_execution()?;

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
        let transaction = Transaction::new(signer, true, false);
        crate::worker::calculate_validator_debt(
            &solana_debt_calculator,
            transaction,
            *epoch,
            *post_to_ledger_only,
        )
        .await?;

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

        let mut solana_connection = SolanaConnection::try_from(solana_connection_options.clone())?;
        solana_connection.cache_if_mainnet().await?;

        let dz_ledger_rpc_client = RpcClient::new_with_commitment(
            dz_ledger_connection_options.dz_ledger_url.clone(),
            CommitmentConfig::confirmed(),
        );

        super::ensure_same_network_environment(&dz_ledger_rpc_client, solana_connection.is_mainnet)
            .await?;

        // Program config on Solana should be the source-of-truth for the current
        // DZ epoch. Presumably, this epoch will be in sync with the DoubleZero
        // Ledger network.
        let latest_distribution_epoch = ZeroCopyAccountOwned::<ProgramConfig>::from_rpc_client(
            &solana_connection,
            &ProgramConfig::find_address().0,
        )
        .await
        .map_err(|_| anyhow!("Revenue Distribution program not initialized"))
        .map(|config| config.data.next_dz_epoch.value().saturating_sub(1))?;

        let target_dz_epoch = epoch.as_ref().copied().unwrap_or(latest_distribution_epoch);
        log_info!("Target DZ epoch: {target_dz_epoch}");

        let rate_limiter = RateLimiter::builder()
            .max(*solana_rate_limit)
            .initial(*solana_rate_limit)
            .refill(*solana_rate_limit)
            .interval(std::time::Duration::from_secs(1))
            .build();

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
                    log_info!("Joined Solana epoch: {solana_epoch}");
                });
            }
            JoinedSolanaEpochs::Duplicate(solana_epoch) => {
                log_warn!("Duplicated joined Solana epoch: {solana_epoch}");
            }
        };

        Ok(())
    }
}
