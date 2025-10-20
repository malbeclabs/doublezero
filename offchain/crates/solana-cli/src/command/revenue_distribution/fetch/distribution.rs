use anyhow::{Context, Result};
use clap::Args;
use doublezero_revenue_distribution::{
    DOUBLEZERO_MINT_DECIMALS, state::Distribution, types::DoubleZeroEpoch,
};
use doublezero_solana_client_tools::{
    rpc::{SolanaConnection, SolanaConnectionOptions},
    zero_copy::ZeroCopyAccountOwned,
};

use crate::command::revenue_distribution::try_fetch_program_config;

#[derive(Debug, Args)]
pub struct DistributionCommand {
    #[arg(long, short = 'e')]
    dz_epoch: Option<u64>,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

#[derive(Debug, tabled::Tabled)]
struct DistributionTableRow {
    field: &'static str,
    value: String,
    note: String,
}

impl DistributionCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self {
            dz_epoch,
            connection_options,
        } = self;

        let connection = SolanaConnection::try_from(connection_options)?;

        let epoch = match dz_epoch {
            Some(epoch) => epoch,
            None => {
                let (_, program_config) = try_fetch_program_config(&connection).await?;

                program_config
                    .next_completed_dz_epoch
                    .value()
                    .saturating_sub(1)
            }
        };

        let (distribution_key, _) = Distribution::find_address(DoubleZeroEpoch::new(epoch));

        let account =
            ZeroCopyAccountOwned::<Distribution>::from_rpc_client(&connection, &distribution_key)
                .await
                .with_context(|| format!("Distribution account not found for epoch {epoch}"))
                .map(|config| config.data.unwrap().0)?;

        let mut value_rows = vec![
            DistributionTableRow {
                field: "Distribution",
                value: epoch.to_string(),
                note: "Epoch of DoubleZero Ledger Network".to_string(),
            },
            DistributionTableRow {
                field: "PDA key",
                value: distribution_key.to_string(),
                note: Default::default(),
            },
            DistributionTableRow {
                field: "Community burn rate",
                value: format!(
                    "{:.7}%",
                    u32::from(account.community_burn_rate) as f64 / 10_000_000.0
                ),
                note: "Lower-bound proportion of rewards burned".to_string(),
            },
        ];

        let fee_parameters = account.solana_validator_fee_parameters;

        if fee_parameters.base_block_rewards_pct != Default::default() {
            value_rows.push(DistributionTableRow {
                field: "Base block rewards fee",
                value: format!(
                    "{:.2}%",
                    u16::from(fee_parameters.base_block_rewards_pct) as f64 / 100.0
                ),
                note: "Amount charged to Solana validators for base block rewards".to_string(),
            });
        }
        if fee_parameters.priority_block_rewards_pct != Default::default() {
            value_rows.push(DistributionTableRow {
                field: "Priority block rewards fee",
                value: format!(
                    "{:.2}%",
                    u16::from(fee_parameters.priority_block_rewards_pct) as f64 / 100.0
                ),
                note: "Amount charged to Solana validators for priority block rewards".to_string(),
            });
        }
        if fee_parameters.inflation_rewards_pct != Default::default() {
            value_rows.push(DistributionTableRow {
                field: "Inflation rewards fee",
                value: format!(
                    "{:.2}%",
                    u16::from(fee_parameters.inflation_rewards_pct) as f64 / 100.0
                ),
                note: "Amount charged to Solana validators for inflation rewards".to_string(),
            });
        }
        if fee_parameters.jito_tips_pct != Default::default() {
            value_rows.push(DistributionTableRow {
                field: "Jito tips fee",
                value: format!(
                    "{:.2}%",
                    u16::from(fee_parameters.jito_tips_pct) as f64 / 100.0
                ),
                note: "Amount charged to Solana validators for Jito tips".to_string(),
            });
        }
        if fee_parameters.fixed_sol_amount != 0 {
            value_rows.push(DistributionTableRow {
                field: "Fixed SOL fee",
                value: format!("{:.9} SOL", fee_parameters.fixed_sol_amount as f64 * 1e-9),
                note: "Fixed SOL amount charged to Solana validators".to_string(),
            });
        }

        value_rows.push(DistributionTableRow {
            field: "Solana validator debt merkle root",
            value: account.solana_validator_debt_merkle_root.to_string(),
            note: Default::default(),
        });

        value_rows.push(DistributionTableRow {
            field: "Total Solana validators",
            value: account.total_solana_validators.to_string(),
            note: Default::default(),
        });

        value_rows.push(DistributionTableRow {
            field: "Solana validator payments count",
            value: account.solana_validator_payments_count.to_string(),
            note: Default::default(),
        });

        value_rows.push(DistributionTableRow {
            field: "Total Solana validator debt",
            value: format!(
                "{:.9} SOL",
                account.total_solana_validator_debt as f64 * 1e-9
            ),
            note: Default::default(),
        });

        value_rows.push(DistributionTableRow {
            field: "Collected Solana validator payments",
            value: format!(
                "{:.9} SOL",
                account.collected_solana_validator_payments as f64 * 1e-9
            ),
            note: Default::default(),
        });

        value_rows.push(DistributionTableRow {
            field: "Rewards merkle root",
            value: account.rewards_merkle_root.to_string(),
            note: Default::default(),
        });

        value_rows.push(DistributionTableRow {
            field: "Total contributors",
            value: account.total_contributors.to_string(),
            note: Default::default(),
        });

        value_rows.push(DistributionTableRow {
            field: "Distributed rewards count",
            value: account.distributed_rewards_count.to_string(),
            note: Default::default(),
        });

        value_rows.push(DistributionTableRow {
            field: "Distributed 2Z amount",
            value: format!(
                "{:.prec$} 2Z",
                account.distributed_2z_amount as f64 / 10f64.powi(DOUBLEZERO_MINT_DECIMALS as i32),
                prec = DOUBLEZERO_MINT_DECIMALS as usize
            ),
            note: Default::default(),
        });
        value_rows.push(DistributionTableRow {
            field: "Burned 2Z amount",
            value: format!(
                "{:.prec$} 2Z",
                account.burned_2z_amount as f64 / 10f64.powi(DOUBLEZERO_MINT_DECIMALS as i32),
                prec = DOUBLEZERO_MINT_DECIMALS as usize
            ),
            note: Default::default(),
        });
        value_rows.push(DistributionTableRow {
            field: "Is debt calculation finalized",
            value: account.is_debt_calculation_finalized().to_string(),
            note: Default::default(),
        });
        value_rows.push(DistributionTableRow {
            field: "Is rewards calculation finalized",
            value: account.is_rewards_calculation_finalized().to_string(),
            note: Default::default(),
        });
        value_rows.push(DistributionTableRow {
            field: "Has swept 2Z tokens",
            value: account.has_swept_2z_tokens().to_string(),
            note: Default::default(),
        });

        super::print_table(value_rows);

        Ok(())
    }
}
