use std::{collections::HashSet, io::Write};

use anyhow::{Context, Result};
use clap::{Args, ValueEnum};
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::{
    account::{record::BorshRecordAccountData, zero_copy::ZeroCopyAccountOwnedData},
    rpc::{DoubleZeroLedgerEnvironmentOverride, SolanaConnection, SolanaConnectionOptions},
};
use doublezero_solana_sdk::revenue_distribution::{
    state::{Distribution, SolanaValidatorDeposit},
    try_is_processed_leaf,
};
use doublezero_solana_validator_debt::{
    rpc::try_fetch_debt_records_and_distributions, validator_debt::ComputedSolanaValidatorDebts,
};
use solana_sdk::{native_token::LAMPORTS_PER_SOL, pubkey::Pubkey};

#[derive(Debug, Clone, PartialEq, Eq, ValueEnum)]
pub enum ValidatorDebtsViewMode {
    Outstanding,
    Node,
    ExcessBalance,
}

#[derive(Debug, Args)]
pub struct ValidatorDebtsCommand {
    #[arg(long, short = 'n', value_name = "PUBKEY")]
    node_id: Option<Pubkey>,

    #[arg(long, value_enum, default_value = "outstanding")]
    view: ValidatorDebtsViewMode,

    #[arg(hide = true, long)]
    debt_accountant: Option<Pubkey>,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,

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
    status: &'static str,
}

impl ValidatorDebtsCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        let Self {
            node_id,
            view,
            debt_accountant: debt_accountant_key,
            connection_options,
            dz_env,
        } = self;

        let solana_connection = crate::command::solana_connection(ctx, &connection_options);

        let (debt_records, distributions) = try_fetch_debt_records_and_distributions(
            &solana_connection,
            dz_env.dz_env,
            debt_accountant_key.as_ref(),
        )
        .await?
        .into_iter()
        .unzip::<_, _, Vec<_>, Vec<_>>();

        match view {
            ValidatorDebtsViewMode::Outstanding | ValidatorDebtsViewMode::ExcessBalance => {
                try_write_validator_debts_outstanding_table(
                    out,
                    &solana_connection,
                    &debt_records,
                    &distributions,
                    node_id.as_ref(),
                    view == ValidatorDebtsViewMode::ExcessBalance,
                )
                .await
            }
            ValidatorDebtsViewMode::Node => {
                let node_id = node_id.context("--node-id is required for --view node")?;
                try_write_validator_debts_node_table(out, &debt_records, &distributions, &node_id)
            }
        }
    }
}

//

async fn try_write_validator_debts_outstanding_table(
    out: &mut impl Write,
    solana_connection: &SolanaConnection,
    debt_records: &[BorshRecordAccountData<ComputedSolanaValidatorDebts>],
    distributions: &[ZeroCopyAccountOwnedData<Distribution>],
    node_id: Option<&Pubkey>,
    excess_mode: bool,
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

    let deposit_account_infos = solana_connection
        .try_fetch_multiple_accounts(&deposit_keys)
        .await?;

    let deposit_balances = deposit_account_infos
        .iter()
        .map(|account_info| {
            doublezero_solana_client_tools::account::balance(account_info, &rent_sysvar)
        })
        .collect::<Vec<_>>();

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
                let bitmap_range = distribution.processed_solana_validator_debt_bitmap_range();
                let processed_leaf_data = &distribution.remaining_data[bitmap_range];

                let is_written_off = if distribution.is_solana_validator_debt_write_off_enabled() {
                    let bitmap_range =
                        distribution.processed_solana_validator_debt_write_off_bitmap_range();
                    let written_off_leaf_data = &distribution.remaining_data[bitmap_range];
                    try_is_processed_leaf(written_off_leaf_data, index).unwrap_or_default()
                } else {
                    false
                };

                // If the debt is not processed or if it is processed but
                // written off, we should include it in the total debt.
                if !try_is_processed_leaf(processed_leaf_data, index).unwrap() || is_written_off {
                    total_debt += debt_record.data.debts[index].amount;
                }
            }
        }

        if excess_mode {
            if total_debt >= deposit_balance {
                continue;
            }

            outputs.push(ValidatorDebtsOutstandingTableRow {
                node_id,
                total_amount: format!("{:.9} SOL", total_debt as f64 * 1e-9),
                deposit_balance: format!("{:.9} SOL", deposit_balance as f64 * 1e-9),
                note: format!(
                    "{:.9} SOL in excess",
                    (deposit_balance - total_debt) as f64 / LAMPORTS_PER_SOL as f64
                ),
            });
        } else {
            if deposit_balance >= total_debt {
                continue;
            }

            outputs.push(ValidatorDebtsOutstandingTableRow {
                node_id,
                total_amount: format!("{:.9} SOL", total_debt as f64 * 1e-9),
                deposit_balance: format!("{:.9} SOL", deposit_balance as f64 * 1e-9),
                note: format!(
                    "{:.9} SOL needed",
                    (total_debt - deposit_balance) as f64 / LAMPORTS_PER_SOL as f64
                ),
            });
        }
    }

    outputs.sort_by_key(|row| row.node_id.to_string());

    if outputs.is_empty() {
        writeln!(out, "No outstanding debts found")?;
    } else {
        super::write_table(
            out,
            outputs,
            super::TableOptions {
                columns_aligned_right: Some(&[1, 2]),
            },
        )?;
    }

    Ok(())
}

fn try_write_validator_debts_node_table(
    out: &mut impl Write,
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
                status: if !is_processed {
                    "unpaid"
                } else if is_written_off {
                    "delinquent"
                } else {
                    "paid"
                },
            });
        }
    }

    outputs.sort_by_key(|row| row.dz_epoch);

    super::write_table(
        out,
        outputs,
        super::TableOptions {
            columns_aligned_right: Some(&[1, 2, 3, 4]),
        },
    )?;

    Ok(())
}
