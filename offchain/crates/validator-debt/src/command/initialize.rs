use anyhow::Result;
use clap::Args;
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, Wallet},
    rpc::DoubleZeroLedgerEnvironmentOverride,
};
use solana_sdk::pubkey::Pubkey;

use crate::worker;

#[derive(Debug, Args, Clone)]
pub struct InitializeDistributionCommand {
    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,

    #[command(flatten)]
    dz_env: DoubleZeroLedgerEnvironmentOverride,

    #[arg(hide = true, long)]
    bypass_dz_epoch_check: bool,

    #[arg(hide = true, long)]
    record_debt_accountant: Option<Pubkey>,
}

impl InitializeDistributionCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self {
            solana_payer_options,
            dz_env,
            bypass_dz_epoch_check,
            record_debt_accountant: record_accountant_key,
        } = self;

        let wallet = Wallet::try_from(solana_payer_options)?;

        worker::try_initialize_distribution(
            wallet,
            dz_env.dz_env,
            bypass_dz_epoch_check,
            record_accountant_key,
        )
        .await
    }
}
