use anyhow::Result;
use clap::Args;
use doublezero_scheduled_command::{Schedulable, ScheduleOption};
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, Wallet},
    rpc::{DoubleZeroLedgerConnection, DoubleZeroLedgerConnectionOptions},
};
use solana_sdk::commitment_config::CommitmentConfig;

use crate::worker;

#[derive(Debug, Args, Clone)]
pub struct InitializeDistributionCommand {
    #[command(flatten)]
    schedule_or_force: super::ScheduleOrForce,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,

    #[command(flatten)]
    dz_ledger_connection_options: DoubleZeroLedgerConnectionOptions,
}

#[async_trait::async_trait]
impl Schedulable for InitializeDistributionCommand {
    fn schedule(&self) -> &ScheduleOption {
        &self.schedule_or_force.schedule
    }

    async fn execute_once(&self) -> Result<()> {
        self.schedule_or_force.ensure_safe_execution()?;

        let wallet = Wallet::try_from(self.solana_payer_options.clone())?;

        let dz_ledger_rpc_client = DoubleZeroLedgerConnection::new_with_commitment(
            self.dz_ledger_connection_options.dz_ledger_url.clone(),
            CommitmentConfig::confirmed(),
        );

        worker::initialize_distribution(wallet, dz_ledger_rpc_client).await?;

        Ok(())
    }
}
