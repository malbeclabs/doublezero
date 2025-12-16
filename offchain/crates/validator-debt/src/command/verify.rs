use anyhow::Result;
use clap::Args;
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, try_load_keypair},
    rpc::{DoubleZeroLedgerConnectionOptions, SolanaConnection, SolanaConnectionOptions},
};
use doublezero_solana_sdk::revenue_distribution::state::ProgramConfig;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::commitment_config::CommitmentConfig;

use crate::{
    rpc::SolanaValidatorDebtConnectionOptions, solana_debt_calculator::SolanaDebtCalculator,
    transaction::Transaction,
};

#[derive(Debug, Args, Clone)]
pub struct VerifyValidatorDebtCommand {
    #[arg(long)]
    epoch: Option<u64>,

    #[arg(long)]
    validator_id: String,

    #[arg(long)]
    amount: u64,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,

    #[command(flatten)]
    dz_ledger_connection_options: DoubleZeroLedgerConnectionOptions,
}

impl VerifyValidatorDebtCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self {
            epoch,
            validator_id,
            amount,
            solana_payer_options,
            dz_ledger_connection_options,
        } = self;

        let epoch = match epoch {
            Some(epoch) => epoch,
            None => {
                latest_distribution_epoch(
                    &solana_payer_options.connection_options,
                    &dz_ledger_connection_options,
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
        let transaction = Transaction::new(signer, true, false);
        crate::worker::verify_validator_debt(
            &solana_debt_calculator,
            transaction,
            epoch,
            validator_id.as_str(),
            amount,
        )
        .await?;

        Ok(())
    }
}

// TODO: Does the dz ledger connection need to be an argument? Also, this is a
// duplicate of the function in calculate.rs.
async fn latest_distribution_epoch(
    solana_connection_options: &SolanaConnectionOptions,
    dz_ledger_connection_options: &DoubleZeroLedgerConnectionOptions,
) -> Result<u64> {
    let solana_connection = SolanaConnection::from(solana_connection_options.clone());
    let is_mainnet = solana_connection
        .try_network_environment()
        .await?
        .is_mainnet_beta();

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
