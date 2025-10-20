use anyhow::Result;
use clap::Args;
use doublezero_revenue_distribution::state::CommunityBurnRateMode;
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};

use crate::command::revenue_distribution::try_fetch_program_config;

#[derive(Debug, Args)]
pub struct ConfigCommand {
    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

#[derive(Debug, tabled::Tabled)]
struct ConfigTableRow {
    field: &'static str,
    value: String,
    note: String,
}

impl ConfigCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self { connection_options } = self;

        let connection = SolanaConnection::try_from(connection_options)?;
        let (program_config_key, program_config) = try_fetch_program_config(&connection).await?;

        if program_config.is_paused() {
            println!("⚠️  Warning: Program is paused");
            println!();
        }

        let distribution_parameters = &program_config.distribution_parameters;
        let community_burn_rate_params = &distribution_parameters.community_burn_rate_parameters;
        let community_burn_rate_mode = community_burn_rate_params.mode();
        let validator_fee_params = &distribution_parameters.solana_validator_fee_parameters;

        let mut value_rows = vec![
            ConfigTableRow {
                field: "PDA key",
                value: program_config_key.to_string(),
                note: Default::default(),
            },
            ConfigTableRow {
                field: "Administrator",
                value: program_config.admin_key.to_string(),
                note: Default::default(),
            },
            ConfigTableRow {
                field: "Debt accountant",
                value: program_config.debt_accountant_key.to_string(),
                note: Default::default(),
            },
            ConfigTableRow {
                field: "Rewards accountant",
                value: program_config.rewards_accountant_key.to_string(),
                note: Default::default(),
            },
            ConfigTableRow {
                field: "Contributor manager",
                value: program_config.contributor_manager_key.to_string(),
                note: Default::default(),
            },
            ConfigTableRow {
                field: "SOL Conversion program",
                value: program_config.sol_2z_swap_program_id.to_string(),
                note: Default::default(),
            },
            ConfigTableRow {
                field: "Calculation grace period",
                value: format!(
                    "{:?}",
                    std::time::Duration::from_secs(
                        u64::from(
                            program_config
                                .distribution_parameters
                                .calculation_grace_period_minutes,
                        ) * 60,
                    )
                ),
                note: Default::default(),
            },
            ConfigTableRow {
                field: "Duration to finalize rewards",
                value: program_config
                    .distribution_parameters
                    .minimum_epoch_duration_to_finalize_rewards
                    .to_string(),
                note: "Minimum number of epochs".to_string(),
            },
            ConfigTableRow {
                field: "Next community burn rate",
                value: format!(
                    "{:.7}% ({})",
                    u32::from(community_burn_rate_params.next_burn_rate().unwrap()) as f64
                        / 10_000_000.0,
                    community_burn_rate_params.mode().to_string().to_lowercase()
                ),
                note: "Determines the next distribution's burn rate".to_string(),
            },
            ConfigTableRow {
                field: "Community burn rate limit",
                value: format!(
                    "{:.7}%",
                    u32::from(community_burn_rate_params.limit) as f64 / 10_000_000.0
                ),
                note: "Determines the maximum burn rate".to_string(),
            },
        ];

        match community_burn_rate_mode {
            CommunityBurnRateMode::Static => {
                value_rows.push(ConfigTableRow {
                    field: "Community burn rate increases after",
                    value: format!(
                        "{} epoch{}",
                        community_burn_rate_params.dz_epochs_to_increasing,
                        if community_burn_rate_params.dz_epochs_to_increasing == 1 {
                            ""
                        } else {
                            "s"
                        },
                    ),
                    note: "Determines the epoch when the community burn rate increases".to_string(),
                });
                value_rows.push(ConfigTableRow {
                    field: "Community burn rate limit reached after",
                    value: format!(
                        "{} epoch{}",
                        community_burn_rate_params.dz_epochs_to_limit,
                        if community_burn_rate_params.dz_epochs_to_limit == 1 {
                            ""
                        } else {
                            "s"
                        }
                    ),
                    note: "Determines the epoch when the community burn rate limit is reached"
                        .to_string(),
                });
            }
            CommunityBurnRateMode::Increasing => {
                value_rows.push(ConfigTableRow {
                    field: "Community burn rate limit reached after",
                    value: format!(
                        "{} epoch{}",
                        community_burn_rate_params.dz_epochs_to_limit,
                        if community_burn_rate_params.dz_epochs_to_limit == 1 {
                            ""
                        } else {
                            "s"
                        }
                    ),
                    note: "Determines the epoch when the community burn rate limit is reached"
                        .to_string(),
                });
            }
            CommunityBurnRateMode::Limit => {}
        }

        let validator_fee_rows = vec![
            ConfigTableRow {
                field: "Solana validator base block rewards fee",
                value: format!(
                    "{:.2}%",
                    u16::from(validator_fee_params.base_block_rewards_pct) as f64 / 100.0
                ),
                note: "Amount charged to Solana validators for base block rewards".to_string(),
            },
            ConfigTableRow {
                field: "Solana validator priority block rewards fee",
                value: format!(
                    "{:.2}%",
                    u16::from(validator_fee_params.priority_block_rewards_pct) as f64 / 100.0
                ),
                note: "Amount charged to Solana validators for priority block rewards".to_string(),
            },
            ConfigTableRow {
                field: "Solana validator inflation rewards fee",
                value: format!(
                    "{:.2}%",
                    u16::from(validator_fee_params.inflation_rewards_pct) as f64 / 100.0
                ),
                note: "Amount charged to Solana validators for inflation rewards".to_string(),
            },
            ConfigTableRow {
                field: "Solana validator Jito tips fee",
                value: format!(
                    "{:.2}%",
                    u16::from(validator_fee_params.jito_tips_pct) as f64 / 100.0
                ),
                note: "Amount charged to Solana validators for Jito tips".to_string(),
            },
            ConfigTableRow {
                field: "Solana validator fixed SOL fee",
                value: format!(
                    "{:.9} SOL",
                    validator_fee_params.fixed_sol_amount as f64 * 1e-9
                ),
                note: "Fixed SOL amount charged to Solana validators".to_string(),
            },
        ];
        value_rows.extend(validator_fee_rows);

        super::print_table(value_rows);

        Ok(())
    }
}

#[derive(Debug, Args)]
pub struct ValidatorFeesCommand {
    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

impl ValidatorFeesCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self { connection_options } = self;
        let connection = SolanaConnection::try_from(connection_options)?;
        let (_, program_config) = try_fetch_program_config(&connection).await?;

        let mut value_rows = Vec::new();

        if let Some(fee_params) = program_config.checked_solana_validator_fee_parameters() {
            if fee_params.base_block_rewards_pct != Default::default() {
                value_rows.push(ConfigTableRow {
                    field: "Base block rewards fee",
                    value: format!(
                        "{:.2}%",
                        u16::from(fee_params.base_block_rewards_pct) as f64 / 100.0
                    ),
                    note: "Amount charged to Solana validators for base block rewards".to_string(),
                });
            }
            if fee_params.priority_block_rewards_pct != Default::default() {
                value_rows.push(ConfigTableRow {
                    field: "Priority block rewards fee",
                    value: format!(
                        "{:.2}%",
                        u16::from(fee_params.priority_block_rewards_pct) as f64 / 100.0
                    ),
                    note: "Amount charged to Solana validators for priority block rewards"
                        .to_string(),
                });
            }
            if fee_params.inflation_rewards_pct != Default::default() {
                value_rows.push(ConfigTableRow {
                    field: "Inflation rewards fee",
                    value: format!(
                        "{:.2}%",
                        u16::from(fee_params.inflation_rewards_pct) as f64 / 100.0
                    ),
                    note: "Amount charged to Solana validators for inflation rewards".to_string(),
                });
            }
            if fee_params.jito_tips_pct != Default::default() {
                value_rows.push(ConfigTableRow {
                    field: "Jito tips fee",
                    value: format!("{:.2}%", u16::from(fee_params.jito_tips_pct) as f64 / 100.0),
                    note: "Amount charged to Solana validators for Jito tips".to_string(),
                });
            }
            if fee_params.fixed_sol_amount != 0 {
                value_rows.push(ConfigTableRow {
                    field: "Fixed SOL fee",
                    value: format!("{:.9} SOL", fee_params.fixed_sol_amount as f64 * 1e-9),
                    note: "Fixed SOL amount charged to Solana validators".to_string(),
                });
            }
        }

        if value_rows.is_empty() {
            println!("... Solana validator fee parameters not configured yet");
            return Ok(());
        }

        super::print_table(value_rows);

        Ok(())
    }
}
