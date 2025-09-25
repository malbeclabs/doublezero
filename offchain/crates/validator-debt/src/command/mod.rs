mod initialize;

//

use std::path::PathBuf;

use anyhow::{Result, bail};
use clap::{Args, Subcommand};
use doublezero_scheduled_command::{Schedulable, ScheduleOption};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{pubkey::Pubkey, signer::keypair::Keypair};

use crate::{
    rpc::SolanaValidatorDebtConnectionOptions, solana_debt_calculator::SolanaDebtCalculator,
    transaction::Transaction, worker,
};

const DOUBLEZERO_LEDGER_GENESIS_HASH: Pubkey =
    solana_sdk::pubkey!("5wVUvkFcFGYiKRUZ8Jp8Wc5swjhDEqT7hTdyssxDpC7P");

#[derive(Debug, Subcommand)]
pub enum ValidatorDebtCommand {
    /// Initialize a new distribution on Solana.
    InitializeDistribution(initialize::InitializeDistributionCommand),

    /// Calculate Validator Debt.
    CalculateValidatorDebt {
        #[command(flatten)]
        solana_connection_options: SolanaValidatorDebtConnectionOptions,
        #[arg(long)]
        epoch: u64,
        #[arg(long, value_name = "DRY_RUN")]
        dry_run: bool,
        #[arg(long, value_name = "FORCE")]
        force: bool,
    },

    /// Finalize Epoch Transaction.
    FinalizeTransaction {
        #[command(flatten)]
        solana_connection_options: SolanaValidatorDebtConnectionOptions,
        #[arg(long)]
        epoch: u64,
        #[arg(long, value_name = "DRY_RUN")]
        dry_run: bool,
        #[arg(long, value_name = "FORCE")]
        force: bool,
    },
}

impl ValidatorDebtCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            ValidatorDebtCommand::InitializeDistribution(cmd) => cmd.execute().await,
            ValidatorDebtCommand::CalculateValidatorDebt {
                solana_connection_options,
                epoch,
                dry_run,
                force,
            } => {
                execute_calculate_validator_debt(solana_connection_options, epoch, dry_run, force)
                    .await
            }
            ValidatorDebtCommand::FinalizeTransaction {
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

async fn execute_calculate_validator_debt(
    solana_connection_options: SolanaValidatorDebtConnectionOptions,
    epoch: u64,
    dry_run: bool,
    force: bool,
) -> Result<()> {
    let solana_debt_calculator: SolanaDebtCalculator =
        SolanaDebtCalculator::try_from(solana_connection_options)?;
    let signer = try_load_keypair(None).expect("failed to load keypair");
    let transaction = Transaction::new(signer, dry_run, force);
    worker::calculate_validator_debt(&solana_debt_calculator, transaction, epoch).await?;
    Ok(())
}

async fn execute_finalize_transaction(
    solana_connection_options: SolanaValidatorDebtConnectionOptions,
    epoch: u64,
    dry_run: bool,
    force: bool,
) -> Result<()> {
    let solana_debt_calculator: SolanaDebtCalculator =
        SolanaDebtCalculator::try_from(solana_connection_options)?;
    let signer = try_load_keypair(None).expect("failed to load keypair");
    let transaction = Transaction::new(signer, dry_run, force);
    worker::finalize_distribution(&solana_debt_calculator, transaction, epoch).await?;
    Ok(())
}

fn try_load_keypair(path: Option<PathBuf>) -> Result<Keypair> {
    let home_path = std::env::var_os("HOME").unwrap();
    let default_keypair_path = ".config/solana/id.json";

    let keypair_path = path.unwrap_or_else(|| PathBuf::from(home_path).join(default_keypair_path));
    try_load_specified_keypair(&keypair_path)
}

fn try_load_specified_keypair(path: &PathBuf) -> Result<Keypair> {
    let keypair_file = std::fs::read_to_string(path)?;
    let keypair_bytes = serde_json::from_str::<Vec<u8>>(&keypair_file)?;
    let default_keypair = Keypair::try_from(keypair_bytes.as_slice())?;

    Ok(default_keypair)
}

//

async fn ensure_same_network_environment(
    dz_ledger_rpc: &RpcClient,
    is_mainnet: bool,
) -> Result<()> {
    let genesis_hash = dz_ledger_rpc.get_genesis_hash().await?;

    // This check is safe to do because there are only two possible DoubleZero
    // Ledger networks: mainnet and testnet.
    if (is_mainnet && genesis_hash.to_bytes() != DOUBLEZERO_LEDGER_GENESIS_HASH.to_bytes())
        || (!is_mainnet && genesis_hash.to_bytes() == DOUBLEZERO_LEDGER_GENESIS_HASH.to_bytes())
    {
        bail!("DoubleZero Ledger environment is not the same as the Solana environment");
    }

    Ok(())
}

#[derive(Debug, Args, Clone)]
struct ScheduleOrForce {
    #[arg(long, short = 'f')]
    force: bool,

    #[command(flatten)]
    schedule: ScheduleOption,
}

impl ScheduleOrForce {
    fn ensure_safe_execution(&self) -> Result<()> {
        if self.schedule.is_scheduled() && self.force {
            bail!("Schedule is not supported with force");
        }

        Ok(())
    }
}
