use std::collections::HashMap;

use anyhow::{Context, Result, ensure};
use clap::{Args, ValueEnum};
use doublezero_revenue_distribution::{
    DOUBLEZERO_MINT_DECIMALS,
    state::{Distribution, SolanaValidatorDeposit},
    types::{DoubleZeroEpoch, UnitShare32},
};
use doublezero_solana_client_tools::{
    rpc::{
        DoubleZeroLedgerConnection, PossibleDoubleZeroLedgerConnectionOptions, SolanaConnection,
        SolanaConnectionOptions,
    },
    zero_copy::ZeroCopyAccountOwnedData,
};
use solana_client::{
    rpc_config::RpcProgramAccountsConfig,
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::{native_token::LAMPORTS_PER_SOL, pubkey::Pubkey};
use tabled::Tabled;

use crate::command::revenue_distribution::{
    fetch::{TableOptions, print_table},
    try_distribution_rewards_iter, try_distribution_solana_validator_debt_iter,
    try_fetch_distribution, try_fetch_program_config, try_fetch_shapley_record,
};

#[derive(Debug, Clone, PartialEq, Eq, ValueEnum)]
pub enum DistributionViewMode {
    Summary,
    ValidatorDebt,
    UnprocessedValidatorDebt,
    Rewards,
}

#[derive(Debug, Args)]
pub struct DistributionCommand {
    #[arg(long, short = 'e')]
    dz_epoch: Option<u64>,

    #[arg(long, value_enum, default_value = "summary")]
    view: DistributionViewMode,

    #[command(flatten)]
    solana_connection_options: SolanaConnectionOptions,

    #[command(flatten)]
    dz_ledger_connection_options: PossibleDoubleZeroLedgerConnectionOptions,

    #[arg(hide = true, long, value_name = "PUBKEY")]
    rewards_accountant: Option<Pubkey>,
}

#[derive(Debug, Tabled)]
struct DistributionSummaryTableRow {
    field: &'static str,
    value: String,
    note: String,
}

#[derive(Debug, Tabled)]
struct DistributionSolanaValidatorDebtTableRow {
    dz_epoch: u64,
    index: usize,
    node_id: String,
    amount: String,
    deposit_balance: String,
    processed: &'static str,
    note: String,
}

#[derive(Debug, Tabled)]
struct DistributionRewardsTableRow {
    dz_epoch: u64,
    index: usize,
    contributor: String,
    proportion: String,
    reward: String,
    distributed: &'static str,
}

impl DistributionCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self {
            dz_epoch,
            view: view_mode,
            solana_connection_options,
            dz_ledger_connection_options,
            rewards_accountant: rewards_accountant_key,
        } = self;

        let solana_connection = SolanaConnection::try_from(solana_connection_options)?;

        let (_, config) = try_fetch_program_config(&solana_connection).await?;

        let epoch = match dz_epoch {
            Some(epoch) => DoubleZeroEpoch::new(epoch),
            None => DoubleZeroEpoch::new(config.next_completed_dz_epoch.value().saturating_sub(1)),
        };

        let (distribution_key, distribution) =
            try_fetch_distribution(&solana_connection, epoch).await?;

        match view_mode {
            DistributionViewMode::Summary => {
                try_print_distribution_summary_table(&distribution_key, &distribution).await
            }
            DistributionViewMode::ValidatorDebt
            | DistributionViewMode::UnprocessedValidatorDebt => {
                ensure!(
                    distribution.is_debt_calculation_finalized(),
                    "Debt calculation is not finalized yet"
                );

                let dz_connection = dz_ledger_connection_options
                    .into_connection()
                    .context("DoubleZero Ledger URL required for --display debt")?;

                try_print_distribution_debt_table(
                    &solana_connection,
                    &dz_connection,
                    &config.debt_accountant_key,
                    &distribution,
                    view_mode == DistributionViewMode::UnprocessedValidatorDebt,
                )
                .await
            }
            DistributionViewMode::Rewards => {
                ensure!(
                    distribution.is_rewards_calculation_finalized(),
                    "Rewards calculation is not finalized yet"
                );

                let dz_connection = dz_ledger_connection_options
                    .into_connection()
                    .context("DoubleZero Ledger URL required for --display rewards")?;

                try_print_distribution_rewards_table(
                    &dz_connection,
                    &rewards_accountant_key.unwrap_or(config.rewards_accountant_key),
                    &distribution,
                )
                .await
            }
        }
    }
}

//

async fn try_print_distribution_summary_table(
    distribution_key: &Pubkey,
    distribution: &Distribution,
) -> Result<()> {
    let mut value_rows = vec![
        DistributionSummaryTableRow {
            field: "Distribution",
            value: distribution.dz_epoch.to_string(),
            note: "Epoch of DoubleZero Ledger Network".to_string(),
        },
        DistributionSummaryTableRow {
            field: "PDA key",
            value: distribution_key.to_string(),
            note: Default::default(),
        },
        DistributionSummaryTableRow {
            field: "Community burn rate",
            value: format!(
                "{:.7}%",
                u32::from(distribution.community_burn_rate) as f64 / 10_000_000.0
            ),
            note: "Lower-bound proportion of rewards burned".to_string(),
        },
    ];

    let fee_parameters = distribution.solana_validator_fee_parameters;

    if fee_parameters.base_block_rewards_pct != Default::default() {
        value_rows.push(DistributionSummaryTableRow {
            field: "Solana validator base block rewards fee",
            value: format!(
                "{:.2}%",
                u16::from(fee_parameters.base_block_rewards_pct) as f64 / 100.0
            ),
            note: "Proportion of base block rewards charged".to_string(),
        });
    }
    if fee_parameters.priority_block_rewards_pct != Default::default() {
        value_rows.push(DistributionSummaryTableRow {
            field: "Solana validator priority block rewards fee",
            value: format!(
                "{:.2}%",
                u16::from(fee_parameters.priority_block_rewards_pct) as f64 / 100.0
            ),
            note: "Proportion of priority block rewards charged".to_string(),
        });
    }
    if fee_parameters.inflation_rewards_pct != Default::default() {
        value_rows.push(DistributionSummaryTableRow {
            field: "Solana validator inflation rewards fee",
            value: format!(
                "{:.2}%",
                u16::from(fee_parameters.inflation_rewards_pct) as f64 / 100.0
            ),
            note: "Proportion of inflation rewards charged".to_string(),
        });
    }
    if fee_parameters.jito_tips_pct != Default::default() {
        value_rows.push(DistributionSummaryTableRow {
            field: "Solana validator Jito tips fee",
            value: format!(
                "{:.2}%",
                u16::from(fee_parameters.jito_tips_pct) as f64 / 100.0
            ),
            note: "Proportion of Jito tips charged".to_string(),
        });
    }
    if fee_parameters.fixed_sol_amount != 0 {
        value_rows.push(DistributionSummaryTableRow {
            field: "Fixed SOL fee",
            value: format!("{:.9} SOL", fee_parameters.fixed_sol_amount as f64 * 1e-9),
            note: "Fixed SOL amount charged".to_string(),
        });
    }

    // Add rows for Solana validator debt if the root has been posted.
    let solana_validator_debt_merkle_root = distribution.solana_validator_debt_merkle_root;
    let has_solana_validator_debt = solana_validator_debt_merkle_root != Default::default();

    if has_solana_validator_debt {
        let unpaid_solana_validators_count =
            distribution.total_solana_validators - distribution.solana_validator_payments_count;

        let more_rows = vec![
            DistributionSummaryTableRow {
                field: "Solana validator debt merkle root",
                value: solana_validator_debt_merkle_root.to_string(),
                note: if distribution.is_debt_calculation_finalized() {
                    "Final".to_string()
                } else {
                    "Staged".to_string()
                },
            },
            DistributionSummaryTableRow {
                field: "Solana validators processed debt count",
                value: format!(
                    "{} / {}",
                    distribution.solana_validator_payments_count,
                    distribution.total_solana_validators,
                ),
                note: format!(
                    "{} {} not paid",
                    unpaid_solana_validators_count,
                    if unpaid_solana_validators_count == 1 {
                        "has"
                    } else {
                        "have"
                    }
                ),
            },
            DistributionSummaryTableRow {
                field: "Total Solana validator payments",
                value: format!(
                    "{:.9} SOL",
                    distribution.collected_solana_validator_payments as f64
                        / LAMPORTS_PER_SOL as f64,
                ),
                note: format!(
                    "{:.3}% collected",
                    distribution.collected_solana_validator_payments as f64 * 100.0
                        / distribution.total_solana_validator_debt as f64
                ),
            },
            DistributionSummaryTableRow {
                field: "Uncollected Solana validator debt",
                value: format!(
                    "{:.9} SOL",
                    (distribution.total_solana_validator_debt
                        - distribution.collected_solana_validator_payments)
                        as f64
                        / LAMPORTS_PER_SOL as f64,
                ),
                note: Default::default(),
            },
        ];
        value_rows.extend(more_rows);
    } else {
        value_rows.push(DistributionSummaryTableRow {
            field: "Solana validator debt merkle root",
            value: solana_validator_debt_merkle_root.to_string(),
            note: if distribution.is_debt_calculation_finalized() {
                "Final".to_string()
            } else {
                "Not posted".to_string()
            },
        });
    }

    // Add rows for rewards if the root has been posted.
    let rewards_merkle_root = distribution.rewards_merkle_root;
    let has_rewards = rewards_merkle_root != Default::default();

    if has_rewards {
        let more_rows = vec![
            DistributionSummaryTableRow {
                field: "Rewards merkle root",
                value: rewards_merkle_root.to_string(),
                note: if distribution.is_rewards_calculation_finalized() {
                    "Final".to_string()
                } else {
                    "Staged".to_string()
                },
            },
            DistributionSummaryTableRow {
                field: "Contributors distributed rewards count",
                value: format!(
                    "{} / {}",
                    distribution.distributed_rewards_count, distribution.total_contributors
                ),
                note: format!(
                    "{} remaining",
                    distribution.total_contributors - distribution.distributed_rewards_count
                ),
            },
            DistributionSummaryTableRow {
                field: "Total distributed rewards",
                value: format!(
                    "{:.1} 2Z",
                    distribution.distributed_2z_amount as f64
                        / f64::powi(10.0, DOUBLEZERO_MINT_DECIMALS as i32),
                ),
                note: Default::default(),
            },
            DistributionSummaryTableRow {
                field: "Total burned rewards",
                value: format!(
                    "{:.1} 2Z",
                    distribution.burned_2z_amount as f64
                        / f64::powi(10.0, DOUBLEZERO_MINT_DECIMALS as i32),
                ),
                note: Default::default(),
            },
            DistributionSummaryTableRow {
                field: "Total remaining 2Z rewards",
                value: format!(
                    "{:.1} 2Z",
                    (distribution.total_collected_2z_tokens()
                        - distribution.distributed_2z_amount
                        - distribution.burned_2z_amount) as f64
                        / f64::powi(10.0, DOUBLEZERO_MINT_DECIMALS as i32),
                ),
                note: Default::default(),
            },
        ];
        value_rows.extend(more_rows);
    } else {
        value_rows.push(DistributionSummaryTableRow {
            field: "Rewards merkle root",
            value: rewards_merkle_root.to_string(),
            note: if distribution.is_rewards_calculation_finalized() {
                "Final".to_string()
            } else {
                "Not posted".to_string()
            },
        });
    }

    print_table(
        value_rows,
        TableOptions {
            columns_aligned_right: Some(&[1]),
        },
    );

    Ok(())
}

async fn try_print_distribution_debt_table(
    solana_connection: &SolanaConnection,
    dz_connection: &DoubleZeroLedgerConnection,
    debt_accountant_key: &Pubkey,
    distribution: &ZeroCopyAccountOwnedData<Distribution>,
    show_unprocessed_only: bool,
) -> Result<()> {
    let dz_epoch = distribution.dz_epoch.value();

    let (_, computed_debt) = doublezero_solana_validator_debt::ledger::try_fetch_debt_record(
        dz_connection,
        debt_accountant_key,
        dz_epoch,
        dz_connection.commitment(),
    )
    .await?;

    if computed_debt.debts.is_empty() {
        println!("No debts found for DZ epoch {dz_epoch}");
        return Ok(());
    }

    let mut debt_rows = Vec::with_capacity(distribution.total_solana_validators as usize);

    let rent = solana_connection
        .get_sysvar::<solana_sdk::rent::Rent>()
        .await?;

    let mut deposit_keys = Vec::with_capacity(computed_debt.debts.len());
    let mut cached_debt_amounts = Vec::with_capacity(computed_debt.debts.len());

    for (leaf_index, debt, is_processed_leaf) in
        try_distribution_solana_validator_debt_iter(distribution, &computed_debt)?
    {
        if show_unprocessed_only && is_processed_leaf {
            continue;
        }

        debt_rows.push(DistributionSolanaValidatorDebtTableRow {
            dz_epoch,
            index: leaf_index,
            node_id: debt.node_id.to_string(),
            amount: format!("{:.9} SOL", debt.amount as f64 / LAMPORTS_PER_SOL as f64),
            deposit_balance: Default::default(),
            processed: if is_processed_leaf { "yes" } else { "no" },
            note: Default::default(),
        });

        deposit_keys.push(SolanaValidatorDeposit::find_address(&debt.node_id).0);
        cached_debt_amounts.push(debt.amount);
    }

    let mut deposit_balances = Vec::with_capacity(debt_rows.len());

    for deposit_keys_chunk in deposit_keys.chunks(100) {
        let balances = solana_connection
            .get_multiple_accounts(deposit_keys_chunk)
            .await?
            .into_iter()
            .flatten()
            .map(|account_info| {
                account_info
                    .lamports
                    .saturating_sub(rent.minimum_balance(account_info.data.len()))
            });
        deposit_balances.extend(balances);
    }

    for ((value_row, debt_amount), deposit_balance) in debt_rows
        .iter_mut()
        .zip(cached_debt_amounts)
        .zip(deposit_balances)
    {
        value_row.deposit_balance = format!(
            "{:.9} SOL",
            deposit_balance as f64 / LAMPORTS_PER_SOL as f64
        );

        if value_row.processed == "yes" {
            continue;
        }

        if deposit_balance < debt_amount {
            if deposit_balance == 0 {
                value_row.note = "Not funded".to_string()
            } else {
                value_row.note = format!(
                    "{:.9} SOL needed",
                    (debt_amount - deposit_balance) as f64 / LAMPORTS_PER_SOL as f64
                );
            }
        }
    }

    print_table(
        debt_rows,
        TableOptions {
            columns_aligned_right: Some(&[0, 1, 3, 4, 5]),
        },
    );

    Ok(())
}

async fn try_print_distribution_rewards_table(
    dz_connection: &DoubleZeroLedgerConnection,
    rewards_accountant_key: &Pubkey,
    distribution: &ZeroCopyAccountOwnedData<Distribution>,
) -> Result<()> {
    let dz_epoch = distribution.dz_epoch;

    // Grab all existing contributors.
    //
    // TODO: Support testnet?
    let mut contributor_label_mapping = dz_connection
        .get_program_accounts_with_config(
            &doublezero_sdk::mainnet::program_id::ID,
            RpcProgramAccountsConfig {
                filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                    0,
                    borsh::to_vec(&doublezero_sdk::AccountType::Contributor)?,
                ))]),
                ..Default::default()
            },
        )
        .await?
        .into_iter()
        .map(|(key, account_info)| {
            let contributor = doublezero_sdk::Contributor::try_from(&account_info.data[..])
                .with_context(|| format!("Failed to deserialize contributor account {key}"))?;
            Ok((contributor.owner, contributor.code))
        })
        .collect::<Result<HashMap<_, _>>>()?;

    let shapley_record =
        try_fetch_shapley_record(dz_connection, rewards_accountant_key, dz_epoch).await?;

    // TODO: Revisit when economic burn rate is introduced.
    let collected_rewards = distribution.total_collected_2z_tokens();
    let burnable_rewards = distribution
        .community_burn_rate
        .mul_scalar(collected_rewards);
    let distributable_rewards = collected_rewards - burnable_rewards;

    let mut rewards_rows = Vec::with_capacity(distribution.total_contributors as usize);

    for (leaf_index, reward_share, is_processed_leaf) in
        try_distribution_rewards_iter(distribution, &shapley_record)?
    {
        let proportion = reward_share.unit_share as f64 / u32::from(UnitShare32::MAX) as f64;

        let unit_share = reward_share.checked_unit_share().unwrap();
        let reward = unit_share.mul_scalar(distributable_rewards) as f64
            / f64::powi(10.0, DOUBLEZERO_MINT_DECIMALS as i32);

        let contributor_label = contributor_label_mapping
            .remove(&reward_share.contributor_key)
            .unwrap_or(reward_share.contributor_key.to_string());

        rewards_rows.push(DistributionRewardsTableRow {
            dz_epoch: dz_epoch.value(),
            index: leaf_index,
            contributor: contributor_label,
            proportion: format!("{:.2}%", 100.0 * proportion),
            reward: format!("{:.1} 2Z", reward),
            distributed: if is_processed_leaf { "yes" } else { "no" },
        });
    }

    print_table(
        rewards_rows,
        TableOptions {
            columns_aligned_right: Some(&[0, 1, 3, 4, 5]),
        },
    );

    Ok(())
}
