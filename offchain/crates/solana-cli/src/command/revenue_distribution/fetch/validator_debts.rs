use std::collections::HashSet;

use anyhow::{Context, Result, ensure};
use clap::{Args, ValueEnum};
use doublezero_solana_client_tools::{
    account::{record::BorshRecordAccountData, zero_copy::ZeroCopyAccountOwnedData},
    rpc::{
        DoubleZeroLedgerConnection, DoubleZeroLedgerEnvironmentOverride, SolanaConnection,
        SolanaConnectionOptions,
    },
};
use doublezero_solana_sdk::revenue_distribution::{
    GENESIS_DZ_EPOCH_MAINNET_BETA,
    state::{Distribution, SolanaValidatorDeposit},
    try_is_processed_leaf,
    types::DoubleZeroEpoch,
};
use doublezero_solana_validator_debt::validator_debt::ComputedSolanaValidatorDebts;
use solana_sdk::{native_token::LAMPORTS_PER_SOL, pubkey::Pubkey};

use crate::command::revenue_distribution::try_fetch_program_config;

#[derive(Debug, Clone, PartialEq, Eq, ValueEnum)]
pub enum ValidatorDebtsViewMode {
    Outstanding,
    Node,
}

#[derive(Debug, Args)]
pub struct ValidatorDebtsCommand {
    #[arg(long, short = 'n', value_name = "PUBKEY")]
    node_id: Option<Pubkey>,

    #[arg(long, value_enum, default_value = "outstanding")]
    view: ValidatorDebtsViewMode,

    #[command(flatten)]
    solana_connection_options: SolanaConnectionOptions,

    #[arg(hide = true, long)]
    debt_accountant: Option<Pubkey>,

    #[command(flatten)]
    dz_env: DoubleZeroLedgerEnvironmentOverride,
}

#[derive(Debug, tabled::Tabled)]
struct ValidatorDebtsOutstandingTableRow {
    node_id: Pubkey,
    total_amount: String,
    deposit_balance: String,
    note: String,
}

#[derive(Debug, tabled::Tabled)]
struct ValidatorDebtsNodeTableRow {
    node_id: Pubkey,
    dz_epoch: u64,
    solana_epoch: String,
    amount: String,
    processed: &'static str,
    written_off: &'static str,
}

impl ValidatorDebtsCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self {
            node_id,
            view,
            solana_connection_options,
            debt_accountant: debt_accountant_key,
            dz_env,
        } = self;

        let solana_connection = SolanaConnection::from(solana_connection_options);

        let (_, config) = try_fetch_program_config(&solana_connection).await?;
        let last_dz_epoch = config.next_completed_dz_epoch.value().saturating_sub(1);

        // Limit to either the last 100 epochs or the default (first) epoch.
        let since_dz_epoch = last_dz_epoch
            .saturating_sub(100)
            .max(GENESIS_DZ_EPOCH_MAINNET_BETA);

        let distribution_keys = (since_dz_epoch..=last_dz_epoch)
            .map(|dz_epoch| Distribution::find_address(DoubleZeroEpoch::new(dz_epoch)).0)
            .collect::<Vec<_>>();

        let distributions = solana_connection
            .get_multiple_accounts(&distribution_keys)
            .await?
            .iter()
            .flatten()
            .filter_map(ZeroCopyAccountOwnedData::<Distribution>::from_account)
            .filter(|distribution| distribution.is_debt_calculation_finalized())
            .collect::<Vec<_>>();

        let network_env = solana_connection.try_network_environment().await?;
        let dz_env = dz_env.dz_env.unwrap_or(network_env);
        let dz_connection = DoubleZeroLedgerConnection::from(dz_env);

        let debt_record_keys = distributions
            .iter()
            .map(|distribution| {
                doublezero_solana_validator_debt::ledger::debt_record_key(
                    &debt_accountant_key.unwrap_or(config.debt_accountant_key),
                    distribution.dz_epoch.value(),
                )
            })
            .collect::<Vec<_>>();

        let debt_records = dz_connection
            .get_multiple_accounts(&debt_record_keys)
            .await?
            .iter()
            .flatten()
            .filter_map(BorshRecordAccountData::<ComputedSolanaValidatorDebts>::from_account)
            .collect::<Vec<_>>();
        ensure!(
            debt_records.len() == distributions.len(),
            "Expected {} debt records, but got {}",
            distributions.len(),
            debt_records.len()
        );

        match view {
            ValidatorDebtsViewMode::Outstanding => {
                try_print_validator_debts_outstanding_table(
                    &solana_connection,
                    &debt_records,
                    &distributions,
                    node_id.as_ref(),
                )
                .await
            }
            ValidatorDebtsViewMode::Node => {
                let node_id = node_id.context("--node-id is required for --view node")?;
                try_print_validator_debts_node_table(&debt_records, &distributions, &node_id)
            }
        }
    }
}

//

async fn try_print_validator_debts_outstanding_table(
    solana_connection: &SolanaConnection,
    debt_records: &[BorshRecordAccountData<ComputedSolanaValidatorDebts>],
    distributions: &[ZeroCopyAccountOwnedData<Distribution>],
    node_id: Option<&Pubkey>,
) -> Result<()> {
    let node_ids = match node_id {
        Some(node_id) => HashSet::from_iter([*node_id]),
        None => debt_records
            .iter()
            .flat_map(|debt_record| debt_record.data.debts.iter().map(|debt| debt.node_id))
            .collect::<HashSet<_>>(),
    };

    let rent_sysvar = solana_connection
        .try_fetch_sysvar::<solana_sdk::rent::Rent>()
        .await?;

    let deposit_keys = node_ids
        .iter()
        .map(|node_id| SolanaValidatorDeposit::find_address(node_id).0)
        .collect::<Vec<_>>();

    let deposit_balances = solana_connection
        .try_fetch_multiple_accounts(&deposit_keys)
        .await?
        .into_iter()
        .map(|account_info| {
            doublezero_solana_client_tools::account::balance(&account_info, &rent_sysvar)
        });

    let mut outputs = Vec::with_capacity(debt_records.len());

    for (node_id, deposit_balance) in node_ids.into_iter().zip(deposit_balances) {
        let mut total_debt = 0;

        for (debt_record, distribution) in debt_records.iter().zip(distributions) {
            if debt_record.debts.is_empty() {
                continue;
            }

            let index = debt_record
                .data
                .debts
                .iter()
                .position(|debt| debt.node_id == node_id);

            if let Some(index) = index {
                let start_index = distribution.processed_solana_validator_debt_start_index as usize;
                let end_index = distribution.processed_solana_validator_debt_end_index as usize;
                let processed_leaf_data = &distribution.remaining_data[start_index..end_index];

                if try_is_processed_leaf(processed_leaf_data, index).unwrap() {
                    continue;
                }

                total_debt += debt_record.data.debts[index].amount;
            }
        }

        if deposit_balance >= total_debt {
            continue;
        }

        let note = if deposit_balance == 0 {
            "Not funded".to_string()
        } else {
            format!(
                "{:.9} SOL needed",
                (total_debt - deposit_balance) as f64 / LAMPORTS_PER_SOL as f64
            )
        };

        outputs.push(ValidatorDebtsOutstandingTableRow {
            node_id,
            total_amount: format!("{:.9} SOL", total_debt as f64 * 1e-9),
            deposit_balance: format!("{:.9} SOL", deposit_balance as f64 * 1e-9),
            note,
        });
    }

    outputs.sort_by_key(|row| row.node_id.to_string());

    if outputs.is_empty() {
        println!("No outstanding debts found");
    } else {
        super::print_table(
            outputs,
            super::TableOptions {
                columns_aligned_right: Some(&[1, 2]),
            },
        );
    }

    Ok(())
}

fn try_print_validator_debts_node_table(
    debt_records: &[BorshRecordAccountData<ComputedSolanaValidatorDebts>],
    distributions: &[ZeroCopyAccountOwnedData<Distribution>],
    node_id: &Pubkey,
) -> Result<()> {
    let mut outputs = Vec::with_capacity(debt_records.len());

    for (computed_debt, distribution) in debt_records.iter().zip(distributions) {
        if computed_debt.debts.is_empty() {
            continue;
        }

        let index = computed_debt
            .debts
            .iter()
            .position(|debt| &debt.node_id == node_id);

        if let Some(index) = index {
            let start_index = distribution.processed_solana_validator_debt_start_index as usize;
            let end_index = distribution.processed_solana_validator_debt_end_index as usize;
            let processed_leaf_data = &distribution.remaining_data[start_index..end_index];

            let is_processed = try_is_processed_leaf(processed_leaf_data, index).unwrap();

            let is_written_off = if distribution.is_solana_validator_debt_write_off_enabled() {
                let start_index =
                    distribution.processed_solana_validator_debt_write_off_start_index as usize;
                let end_index =
                    distribution.processed_solana_validator_debt_write_off_end_index as usize;
                let written_off_leaf_data = &distribution.remaining_data[start_index..end_index];
                try_is_processed_leaf(written_off_leaf_data, index).unwrap()
            } else {
                false
            };

            let debt = &computed_debt.debts[index];

            // Unlikely to happen, but there can be multiple Solana epochs per
            // DZ epoch.
            let solana_epoch = (computed_debt.first_solana_epoch..=computed_debt.last_solana_epoch)
                .map(|epoch| epoch.to_string())
                .collect::<Vec<_>>()
                .join(",");

            outputs.push(ValidatorDebtsNodeTableRow {
                node_id: *node_id,
                dz_epoch: distribution.dz_epoch.value(),
                solana_epoch,
                amount: format!("{:.9} SOL", debt.amount as f64 * 1e-9),
                processed: if is_processed { "yes" } else { "no" },
                written_off: if is_written_off { "yes" } else { "no" },
            });
        }
    }

    outputs.sort_by_key(|row| row.dz_epoch);

    super::print_table(
        outputs,
        super::TableOptions {
            columns_aligned_right: Some(&[1, 2, 3, 4, 5]),
        },
    );

    Ok(())
}
