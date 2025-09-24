use anyhow::Result;
use borsh::de::BorshDeserialize;
use clap::{Args, Subcommand};
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, Wallet},
    rpc::DoubleZeroLedgerConnectionOptions,
};
use doublezero_solana_validator_debt::{
    ledger, transaction::Transaction, validator_debt::ComputedSolanaValidatorDebts,
};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_commitment_config::CommitmentConfig;

#[derive(Debug, Args)]
pub struct RevenueDistributionRelayCommand {
    #[command(subcommand)]
    pub inner: RevenueDistributionRelaySubcommand,
}

#[derive(Debug, Subcommand)]
pub enum RevenueDistributionRelaySubcommand {
    PaySolanaValidatorDebt {
        #[arg(long)]
        epoch: u64,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,

        #[command(flatten)]
        dz_ledger_connection_options: DoubleZeroLedgerConnectionOptions,
    },
    // TODO: Add `DistributeRewards`
    // TODO: Add `SweepDistributionTokens`
}

impl RevenueDistributionRelaySubcommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::PaySolanaValidatorDebt {
                epoch,
                solana_payer_options,
                dz_ledger_connection_options,
            } => {
                execute_pay_solana_validator_debt(
                    epoch,
                    solana_payer_options,
                    dz_ledger_connection_options,
                )
                .await
            }
        }
    }
}

pub async fn execute_pay_solana_validator_debt(
    epoch: u64,
    solana_payer_options: SolanaPayerOptions,
    dz_ledger_connection_options: DoubleZeroLedgerConnectionOptions,
) -> Result<()> {
    let prefix = b"solana_validator_debt_test";
    let dz_epoch_bytes = epoch.to_le_bytes();
    let seeds: &[&[u8]] = &[prefix, &dz_epoch_bytes];
    let wallet = Wallet::try_from(solana_payer_options)?;
    let dz_ledger_rpc_client = RpcClient::new_with_commitment(
        dz_ledger_connection_options.dz_ledger_url,
        CommitmentConfig::confirmed(),
    );
    let read = ledger::read_from_ledger(
        &dz_ledger_rpc_client,
        &wallet.signer,
        seeds,
        dz_ledger_rpc_client.commitment(),
    )
    .await?;

    let deserialized = ComputedSolanaValidatorDebts::try_from_slice(read.1.as_slice())?;

    let transaction = Transaction::new(wallet.signer, wallet.dry_run, false); // hardcoding force as false as it doesn't matter here. will revisit later
    let transactions = transaction
        .pay_solana_validator_debt(&wallet.connection.rpc_client, deserialized, epoch)
        .await?;
    for t in transactions {
        transaction
            .send_or_simulate_transaction(&wallet.connection.rpc_client, &t)
            .await?;
    }
    Ok(())
}
