mod distribute_rewards;
mod finalize_distribution_rewards;
mod sweep_distribution_tokens;

//

use anyhow::Result;
use borsh::de::BorshDeserialize;
use chrono::Utc;
use clap::{Args, Subcommand, ValueEnum};
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    ID,
    instruction::{
        RevenueDistributionInstructionData, account::InitializeSolanaValidatorDepositAccounts,
    },
    state::SolanaValidatorDeposit,
};
use doublezero_scheduled_command::Schedulable;
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, Wallet},
    rpc::DoubleZeroLedgerConnectionOptions,
};
use doublezero_solana_validator_debt::{
    ledger,
    transaction::{SOLANA_SEED_PREFIX, Transaction},
    validator_debt::ComputedSolanaValidatorDebts,
};
use slack_notifier::validator_debt;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};

#[derive(Debug, Clone, ValueEnum)]
pub enum ExportFormat {
    Csv,
    Slack,
}

#[derive(Debug, Args)]
pub struct RevenueDistributionRelayCommand {
    #[command(subcommand)]
    pub inner: RevenueDistributionRelaySubcommand,
}

#[derive(Debug, Subcommand)]
pub enum RevenueDistributionRelaySubcommand {
    // TODO: add schedule
    PaySolanaValidatorDebt {
        #[arg(long)]
        dz_epoch: u64,

        /// export results: csv, slack
        #[arg(long, value_enum)]
        export: Option<ExportFormat>,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,

        #[command(flatten)]
        dz_ledger_connection_options: DoubleZeroLedgerConnectionOptions,
    },

    SweepDistributionTokens(sweep_distribution_tokens::SweepDistributionTokens),

    FinalizeDistributionRewards(finalize_distribution_rewards::FinalizeDistributionRewards),

    DistributeRewards(distribute_rewards::DistributeRewards),
}

impl RevenueDistributionRelaySubcommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::PaySolanaValidatorDebt {
                dz_epoch,
                solana_payer_options,
                dz_ledger_connection_options,
                export,
            } => {
                execute_pay_solana_validator_debt(
                    dz_epoch,
                    solana_payer_options,
                    dz_ledger_connection_options,
                    export,
                )
                .await
            }
            Self::SweepDistributionTokens(command) => command.execute().await,
            Self::FinalizeDistributionRewards(command) => command.execute().await,
            Self::DistributeRewards(command) => command.execute().await,
        }
    }
}

async fn execute_pay_solana_validator_debt(
    epoch: u64,
    solana_payer_options: SolanaPayerOptions,
    dz_ledger_connection_options: DoubleZeroLedgerConnectionOptions,
    export: Option<ExportFormat>,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;

    let dz_ledger_rpc_client = RpcClient::new_with_commitment(
        dz_ledger_connection_options.dz_ledger_url,
        CommitmentConfig::confirmed(),
    );
    let (_, record_data) = ledger::read_from_ledger(
        &dz_ledger_rpc_client,
        &wallet.signer,
        &[SOLANA_SEED_PREFIX, &epoch.to_le_bytes()],
        dz_ledger_rpc_client.commitment(),
    )
    .await?;

    let computed_debt = ComputedSolanaValidatorDebts::try_from_slice(&record_data)?;

    try_initialize_missing_deposit_accounts(&wallet, &computed_debt).await?;

    let transaction = Transaction::new(wallet.signer, wallet.dry_run, false); // hardcoding force as false as it doesn't matter here. will revisit later
    let tx_results = transaction
        .pay_solana_validator_debt(&wallet.connection.rpc_client, computed_debt, epoch)
        .await?;

    let mut filename: Option<String> = None;

    if let Some(ExportFormat::Csv) = export {
        let now = Utc::now();
        let timestamp_milliseconds: i64 = now.timestamp_millis();
        let string_filename = if wallet.dry_run {
            format!("DRY_RUN_dz_epoch_{epoch}_pay_solana_debt_{timestamp_milliseconds}.csv")
        } else {
            format!("dz_epoch_{epoch}_pay_solana_debt_{timestamp_milliseconds}.csv")
        };
        let mut writer = csv::Writer::from_path(string_filename.clone())?;

        for tx_result in tx_results.collection_results {
            writer.serialize(tx_result)?;
        }
        filename = Some(string_filename);
        writer.flush()?;
    };
    if let Some(ExportFormat::Slack) = export {
        validator_debt::post_debt_collection_to_slack(
            tx_results.total_transactions_attempted,
            tx_results.successful_transactions,
            tx_results.insufficient_funds,
            tx_results.already_paid,
            epoch,
            filename,
            wallet.dry_run,
        )
        .await?;
    }

    Ok(())
}

async fn try_initialize_missing_deposit_accounts(
    wallet: &Wallet,
    computed_debt: &ComputedSolanaValidatorDebts,
) -> Result<()> {
    let wallet_key = wallet.pubkey();

    let node_ids = computed_debt
        .debts
        .iter()
        .map(|debt| debt.node_id)
        .collect::<Vec<_>>();

    let mut uninitialized_items = Vec::<(Pubkey, (Pubkey, u8))>::new();

    for node_ids_chunk in node_ids.chunks(100) {
        let deposit_keys_and_bumps = node_ids_chunk
            .iter()
            .map(SolanaValidatorDeposit::find_address)
            .collect::<Vec<_>>();
        let deposit_accounts = wallet
            .connection
            .get_multiple_accounts(
                &deposit_keys_and_bumps
                    .iter()
                    .map(|(key, _)| key)
                    .copied()
                    .collect::<Vec<_>>(),
            )
            .await?;

        uninitialized_items.extend(
            deposit_accounts
                .iter()
                .zip(deposit_keys_and_bumps)
                .zip(node_ids_chunk.iter().copied())
                .filter_map(|((account, deposit_key_and_bump), node_id)| {
                    if account.is_none() {
                        Some((node_id, deposit_key_and_bump))
                    } else {
                        None
                    }
                }),
        );
    }

    for uninitialized_items_chunk in uninitialized_items.chunks(16) {
        let mut instructions = Vec::new();
        let mut compute_unit_limit = 5_000;

        for (node_id, (deposit_key, bump)) in uninitialized_items_chunk {
            let ix = try_build_instruction(
                &ID,
                InitializeSolanaValidatorDepositAccounts {
                    new_solana_validator_deposit_key: *deposit_key,
                    payer_key: wallet_key,
                },
                &RevenueDistributionInstructionData::InitializeSolanaValidatorDeposit(*node_id),
            )?;
            instructions.push(ix);
            compute_unit_limit += 10_000 + Wallet::compute_units_for_bump_seed(*bump);
        }

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
            compute_unit_limit,
        ));

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let Some(tx_sig) = tx_sig {
            println!("Initialize Solana validator deposit: {tx_sig}");

            wallet.print_verbose_output(&[tx_sig]).await?;
        }
    }

    Ok(())
}
