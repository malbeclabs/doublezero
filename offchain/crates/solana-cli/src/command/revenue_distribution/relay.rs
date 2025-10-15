use anyhow::Result;
use borsh::de::BorshDeserialize;
use chrono::Utc;
use clap::{Args, Subcommand, ValueEnum};
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, Wallet},
    rpc::DoubleZeroLedgerConnectionOptions,
};
use doublezero_solana_validator_debt::{
    ledger,
    transaction::{SOLANA_SEED_PREFIX, Transaction},
    validator_debt::ComputedSolanaValidatorDebts,
};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_commitment_config::CommitmentConfig;

#[derive(Debug, Clone, ValueEnum)]
pub enum ExportFormat {
    Csv,
}

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

        /// export results: csv
        #[arg(long, value_enum)]
        export: Option<ExportFormat>,

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
                export,
            } => {
                execute_pay_solana_validator_debt(
                    epoch,
                    solana_payer_options,
                    dz_ledger_connection_options,
                    export,
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
    export: Option<ExportFormat>,
) -> Result<()> {
    let dz_epoch_bytes = epoch.to_le_bytes();
    let seeds: &[&[u8]] = &[SOLANA_SEED_PREFIX, &dz_epoch_bytes];
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
    let tx_results = transaction
        .pay_solana_validator_debt(&wallet.connection.rpc_client, deserialized, epoch)
        .await?;

    if let Some(ExportFormat::Csv) = export {
        let now = Utc::now();
        let timestamp_milliseconds: i64 = now.timestamp_millis();
        let filename = format!("dz_epoch_{epoch}_{timestamp_milliseconds}.csv");
        let mut writer = csv::Writer::from_path(filename)?;

        for tx_result in tx_results {
            writer.serialize(tx_result)?;
        }

        writer.flush()?;
    };

    Ok(())
}
