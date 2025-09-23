use std::path::PathBuf;

use anyhow::{Result, bail};
use clap::{Args, Subcommand};
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    ID as REVENUE_DISTRIBUTION_PROGRAM_ID,
    instruction::{RevenueDistributionInstructionData, account::InitializeDistributionAccounts},
    state::{self, Distribution, ProgramConfig},
};
use doublezero_scheduled_command::{Schedulable, ScheduleOption};
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, Wallet},
    rpc::DoubleZeroLedgerConnectionOptions,
    zero_copy::ZeroCopyAccountOwned,
};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{
    commitment_config::CommitmentConfig,
    compute_budget::ComputeBudgetInstruction,
    pubkey::Pubkey,
    signer::{Signer, keypair::Keypair},
};

use crate::{
    rpc::SolanaValidatorDebtConnectionOptions, solana_debt_calculator::SolanaDebtCalculator,
    transaction::Transaction, worker,
};

const DOUBLEZERO_LEDGER_GENESIS_HASH: Pubkey =
    solana_sdk::pubkey!("5wVUvkFcFGYiKRUZ8Jp8Wc5swjhDEqT7hTdyssxDpC7P");

#[derive(Debug, Subcommand)]
pub enum ValidatorDebtCommand {
    /// Initialize a new distribution on Solana.
    InitializeDistribution(InitializeDistributionCommand),

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

#[derive(Debug, Args, Clone)]
pub struct InitializeDistributionCommand {
    #[command(flatten)]
    schedule_or_force: ScheduleOrForce,

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

        let (next_dz_epoch, expected_accountant_key) =
            ZeroCopyAccountOwned::<ProgramConfig>::from_rpc_client(
                &wallet.connection,
                &ProgramConfig::find_address().0,
            )
            .await
            .map(|config| (config.data.next_dz_epoch, config.data.debt_accountant_key))?;

        if wallet.signer.pubkey() != expected_accountant_key {
            bail!("Signer does not match expected debt accountant");
        }

        let dz_ledger_rpc_client = RpcClient::new_with_commitment(
            self.dz_ledger_connection_options.dz_ledger_url.clone(),
            CommitmentConfig::confirmed(),
        );

        ensure_same_network_environment(&dz_ledger_rpc_client, wallet.connection.is_mainnet)
            .await?;

        let expected_dz_epoch = dz_ledger_rpc_client.get_epoch_info().await?.epoch;

        // Ensure that the epoch from the DoubleZero Ledger network equals the next
        // one known by the Revenue Distribution program. If it does not, this
        // method has not been called for a long time.
        if next_dz_epoch.value() != expected_dz_epoch {
            if self.schedule_or_force.force {
                tracing::warn!("DZ epoch {expected_dz_epoch} != program's epoch {next_dz_epoch}");
            } else {
                bail!("DZ epoch {expected_dz_epoch} != program's epoch {next_dz_epoch}");
            }
        }

        let dz_mint_key = if wallet.connection.is_mainnet {
            doublezero_revenue_distribution::env::mainnet::DOUBLEZERO_MINT_KEY
        } else {
            doublezero_revenue_distribution::env::development::DOUBLEZERO_MINT_KEY
        };

        let initialize_distribution_ix = try_build_instruction(
            &REVENUE_DISTRIBUTION_PROGRAM_ID,
            InitializeDistributionAccounts::new(
                &expected_accountant_key,
                &expected_accountant_key,
                next_dz_epoch,
                &dz_mint_key,
            ),
            &RevenueDistributionInstructionData::InitializeDistribution,
        )
        .unwrap();

        let mut compute_unit_limit = 24_000;

        let (distribution_key, bump) = Distribution::find_address(next_dz_epoch);
        compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

        let (_, bump) = state::find_2z_token_pda_address(&distribution_key);
        compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

        let instructions = vec![
            initialize_distribution_ix,
            ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
            ComputeBudgetInstruction::set_compute_unit_price(1_000_000), // Land it.
        ];

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let Some(tx_sig) = tx_sig {
            println!("Initialize distribution: {tx_sig}");

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
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
