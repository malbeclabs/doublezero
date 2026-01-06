mod calculate;
mod export_validators;
mod initialize;
mod verify;

//

use anyhow::{Result, bail};
use doublezero_solana_client_tools::payer::try_load_keypair;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::pubkey::Pubkey;

use crate::{
    rpc::SolanaValidatorDebtConnectionOptions, solana_debt_calculator::SolanaDebtCalculator,
    transaction::Transaction, worker,
};

const DOUBLEZERO_LEDGER_MAINNET_BETA_GENESIS_HASH: Pubkey =
    solana_sdk::pubkey!("5wVUvkFcFGYiKRUZ8Jp8Wc5swjhDEqT7hTdyssxDpC7P");

#[derive(Debug, clap::Subcommand)]
pub enum ValidatorDebtCommand {
    /// Calculate Validator Debt.
    CalculateValidatorDebt(calculate::CalculateValidatorDebtCommand),

    FindSolanaEpoch(calculate::FindSolanaEpochCommand),

    VerifyValidatorDebt(verify::VerifyValidatorDebtCommand),

    /// Export validator pubkeys for a given Solana epoch.
    ExportValidators(export_validators::ExportValidatorsCommand),

    /// Finalize Epoch Distribution.
    FinalizeDistribution {
        #[command(flatten)]
        solana_connection_options: SolanaValidatorDebtConnectionOptions,
        #[arg(long)]
        epoch: u64,
        #[arg(long, value_name = "DRY_RUN")]
        dry_run: bool,
        #[arg(long, value_name = "FORCE")]
        force: bool,
    },

    // Initialize a new distribution on Solana.
    //
    // TODO: Consider only allowing localnet for this command since the
    // scheduler handles initialization.
    #[command(hide = true)]
    InitializeDistribution(initialize::InitializeDistributionCommand),
}

impl ValidatorDebtCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            ValidatorDebtCommand::InitializeDistribution(command) => {
                command.try_into_execute().await
            }
            ValidatorDebtCommand::CalculateValidatorDebt(command) => {
                command.try_into_execute().await
            }
            ValidatorDebtCommand::FindSolanaEpoch(command) => command.try_into_execute().await,
            ValidatorDebtCommand::VerifyValidatorDebt(command) => command.try_into_execute().await,
            ValidatorDebtCommand::ExportValidators(command) => command.try_into_execute().await,
            ValidatorDebtCommand::FinalizeDistribution {
                solana_connection_options,
                epoch,
                dry_run,
                force,
            } => {
                execute_finalize_transaction(solana_connection_options, epoch, dry_run, force).await
            }
        }
    }
}

async fn execute_finalize_transaction(
    solana_connection_options: SolanaValidatorDebtConnectionOptions,
    epoch: u64,
    dry_run: bool,
    force: bool,
) -> Result<()> {
    let solana_debt_calculator: SolanaDebtCalculator =
        SolanaDebtCalculator::try_from(solana_connection_options)?;
    let signer = try_load_keypair(None)?;
    let transaction = Transaction::new(signer.into(), dry_run, force);
    worker::finalize_distribution(&solana_debt_calculator, transaction, epoch).await?;
    Ok(())
}

//

async fn ensure_same_network_environment(
    dz_ledger_rpc: &RpcClient,
    is_mainnet: bool,
) -> Result<()> {
    let genesis_hash = dz_ledger_rpc.get_genesis_hash().await?;

    // This check is safe to do because there are only two possible DoubleZero
    // Ledger networks: mainnet and testnet.
    if (is_mainnet
        && genesis_hash.to_bytes() != DOUBLEZERO_LEDGER_MAINNET_BETA_GENESIS_HASH.to_bytes())
        || (!is_mainnet
            && genesis_hash.to_bytes() == DOUBLEZERO_LEDGER_MAINNET_BETA_GENESIS_HASH.to_bytes())
    {
        bail!("DoubleZero Ledger environment is not the same as the Solana environment");
    }

    Ok(())
}
