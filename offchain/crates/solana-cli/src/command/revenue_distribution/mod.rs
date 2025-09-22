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
use doublezero_solana_client_tools::zero_copy::ZeroCopyAccountOwned;
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, Wallet},
    rpc::{DoubleZeroLedgerConnectionOptions, SolanaConnection, SolanaConnectionOptions},
};
use doublezero_solana_validator_debt::{
    ledger, transaction::Transaction, validator_debt::ComputedSolanaValidatorDebts,
};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};

#[derive(Debug, Args)]
pub struct RevenueDistributionCliCommand {
    #[command(subcommand)]
    pub command: RevenueDistributionSubCommand,
}

#[derive(Debug, Subcommand)]
pub enum RevenueDistributionSubCommand {
    Fetch {
        #[arg(long)]
        config: bool,

        #[arg(long)]
        journal: bool,

        #[arg(long)]
        solana_validator_fees: bool,

        // TODO: --distribution with Option<u64>.
        // TODO: --contributor-rewards with Option<Pubkey>.
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
        dz_ledger_connection_options: DoubleZeroLedgerConnectionOptions,
    },
}

impl RevenueDistributionSubCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            RevenueDistributionSubCommand::Fetch {
                config,
                journal,
                solana_validator_fees,
                solana_connection_options,
            } => {
                execute_fetch(
                    config,
                    journal,
                    solana_validator_fees,
                    solana_connection_options,
                )
                .await
            }
            RevenueDistributionSubCommand::InitializeContributorRewards {
                service_key,
                solana_payer_options,
            } => execute_initialize_contributor_rewards(service_key, solana_payer_options).await,
            RevenueDistributionSubCommand::PaySolanaValidatorDebt {
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

//
// RevenueDistributionSubCommand::Fetch.
//

async fn execute_fetch(
    config: bool,
    journal: bool,
    solana_validator_fees: bool,
    solana_connection_options: SolanaConnectionOptions,
) -> Result<()> {
    let connection = SolanaConnection::try_from(solana_connection_options)?;

    if config {
        let program_config = fetch_program_config(&connection).await?;

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

    if solana_validator_fees {
        let program_config = fetch_program_config(&connection).await?;

        let fee_params = program_config
            .checked_solana_validator_fee_parameters()
            .ok_or(anyhow!(
                "Solana validator fee parameters not configured yet"
            ))?;

        println!("Fee Parameter          | Value");
        println!("-----------------------|----------------");
        if fee_params.base_block_rewards_pct != Default::default() {
            println!(
                "Base block rewards     | {:.2}%",
                u16::from(fee_params.base_block_rewards_pct) as f64 / 100.0
            );
        }
        if fee_params.priority_block_rewards_pct != Default::default() {
            println!(
                "Priority block rewards | {:.2}%",
                u16::from(fee_params.priority_block_rewards_pct) as f64 / 100.0
            );
        }
        if fee_params.inflation_rewards_pct != Default::default() {
            println!(
                "Inflation rewards      | {:.2}%",
                u16::from(fee_params.inflation_rewards_pct) as f64 / 100.0
            );
        }
        if fee_params.jito_tips_pct != Default::default() {
            println!(
                "Jito tips              | {:.2}%",
                u16::from(fee_params.jito_tips_pct) as f64 / 100.0
            );
        }
        if fee_params.fixed_sol_amount != 0 {
            println!(
                "Fixed                  | {:.9} SOL",
                fee_params.fixed_sol_amount as f64 * 1e-9
            );
        }
        println!();
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

//

async fn fetch_program_config(connection: &SolanaConnection) -> Result<ProgramConfig> {
    let program_config = ZeroCopyAccountOwned::from_rpc_client(
        &connection.rpc_client,
        &ProgramConfig::find_address().0,
    )
    .await?;

    Ok(program_config.data)
}
