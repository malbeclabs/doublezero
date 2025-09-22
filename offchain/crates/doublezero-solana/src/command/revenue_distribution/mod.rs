use anyhow::{Result, anyhow};
use borsh::de::BorshDeserialize;
use clap::{Args, Subcommand};
use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};
use doublezero_revenue_distribution::{
    ID,
    instruction::{
        RevenueDistributionInstructionData, account::InitializeContributorRewardsAccounts,
    },
    state::{ContributorRewards, Journal, ProgramConfig},
};

use doublezero_solana_validator_debt::{
    ledger, transaction::Transaction, validator_debt::ComputedSolanaValidatorDebts,
};

use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};

use crate::{
    payer::{SolanaPayerOptions, Wallet},
    rpc::{Connection, LedgerConnection, LedgerConnectionOptions, SolanaConnectionOptions},
};

#[derive(Debug, Args)]
pub struct RevenueDistributionCliCommand {
    #[command(subcommand)]
    pub command: RevenueDistributionSubCommand,
}

#[derive(Debug, Subcommand)]
pub enum RevenueDistributionSubCommand {
    Fetch {
        #[arg(long)]
        program_config: bool,

        #[arg(long)]
        journal: bool,

        // TODO: --distribution with Option<u64>.
        // TODO: --contributor-rewards with Option<Pubkey>.
        // TODO: --prepaid-connection with Option<Pubkey>.
        //
        #[command(flatten)]
        solana_connection_options: SolanaConnectionOptions,
    },

    /// Initialize contributor rewards account for a contributor's service key.
    InitializeContributorRewards {
        service_key: Pubkey,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    PaySolanaValidatorDebt {
        #[arg(long)]
        epoch: u64,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
        #[command(flatten)]
        ledger_connection_options: LedgerConnectionOptions,
    },
}

impl RevenueDistributionSubCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            RevenueDistributionSubCommand::Fetch {
                program_config,
                journal,
                solana_connection_options,
            } => execute_fetch(program_config, journal, solana_connection_options).await,
            RevenueDistributionSubCommand::InitializeContributorRewards {
                service_key,
                solana_payer_options,
            } => execute_initialize_contributor_rewards(service_key, solana_payer_options).await,
            RevenueDistributionSubCommand::PaySolanaValidatorDebt {
                epoch,
                solana_payer_options,
                ledger_connection_options,
            } => {
                execute_pay_solana_validator_debt(
                    epoch,
                    solana_payer_options,
                    ledger_connection_options,
                )
                .await
            }
        }
    }
}

//
// RevenueDistributionSubCommand::Fetch.
//

async fn execute_fetch(
    program_config: bool,
    journal: bool,
    solana_connection_options: SolanaConnectionOptions,
) -> Result<()> {
    let connection = Connection::try_from(solana_connection_options)?;

    if program_config {
        let program_config_key = ProgramConfig::find_address().0;
        let program_config_info = connection.get_account(&program_config_key).await?;

        let (program_config, _) =
            zero_copy::checked_from_bytes_with_discriminator::<ProgramConfig>(
                &program_config_info.data,
            )
            .ok_or(anyhow!("Failed to deserialize program config"))?;

        // TODO: Pretty print.
        println!("Program config: {program_config:?}");
    }

    if journal {
        let journal_key = Journal::find_address().0;
        let journal_info = connection.get_account(&journal_key).await?;

        let (journal, _) =
            zero_copy::checked_from_bytes_with_discriminator::<Journal>(&journal_info.data)
                .ok_or(anyhow!("Failed to deserialize journal"))?;

        // TODO: Pretty print.
        println!("Journal: {journal:?}");
    }

    Ok(())
}

//
// RevenueDistributionSubCommand::InitializeContributorRewards.
//

pub async fn execute_initialize_contributor_rewards(
    service_key: Pubkey,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let wallet_key = wallet.pubkey();

    let initialize_contributor_rewards_ix = try_build_instruction(
        &ID,
        InitializeContributorRewardsAccounts::new(&wallet_key, &service_key),
        &RevenueDistributionInstructionData::InitializeContributorRewards(service_key),
    )?;

    let mut compute_unit_limit = 10_000;

    let (_, bump) = ContributorRewards::find_address(&service_key);
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let mut instructions = vec![
        initialize_contributor_rewards_ix,
        ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
    ];

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let Some(tx_sig) = tx_sig {
        println!("Initialized contributor rewards: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

//
// RevenueDistributionSubCommand::PaySolanaValidatorDebt.
//
//
pub async fn execute_pay_solana_validator_debt(
    epoch: u64,
    solana_payer_options: SolanaPayerOptions,
    ledger_connection_options: LedgerConnectionOptions,
) -> Result<()> {
    let prefix = b"solana_validator_debt_test";
    let dz_epoch_bytes = epoch.to_le_bytes();
    let seeds: &[&[u8]] = &[prefix, &dz_epoch_bytes];
    let wallet = Wallet::try_from(solana_payer_options)?;
    let ledger = LedgerConnection::try_from(ledger_connection_options)?;
    let read = ledger::read_from_ledger(
        &ledger.rpc_client,
        &wallet.signer,
        seeds,
        ledger.rpc_client.commitment(),
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
