use anyhow::{Result, anyhow};
use clap::Args;
use doublezero_program_tools::zero_copy;
use doublezero_revenue_distribution::state::{CommunityBurnRateMode, Journal};
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Args)]
pub struct FetchCommand {
    #[arg(long)]
    config: bool,

    #[arg(long)]
    journal: bool,

    #[arg(long)]
    solana_validator_fees: bool,

    #[arg(long, value_name = "PUBKEY")]
    solana_validator_deposit: Option<Pubkey>,

    // TODO: --distribution with Option<u64>.
    // TODO: --contributor-rewards with Option<Pubkey>.
    //
    #[command(flatten)]
    solana_connection_options: SolanaConnectionOptions,
}

impl FetchCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let FetchCommand {
            config,
            journal,
            solana_validator_fees,
            solana_validator_deposit,
            solana_connection_options,
        } = self;

        let connection = SolanaConnection::try_from(solana_connection_options)?;

        if config {
            let (program_config_key, program_config) =
                super::try_fetch_program_config(&connection).await?;

            println!("Program config: {program_config_key}");
            println!();

            println!("Parameter                                   | Value");
            println!(
                "--------------------------------------------+-------------------------------------------------"
            );
            println!(
                "Is program paused?                          | {}",
                program_config.is_paused()
            );
            println!(
                "Admin key                                   | {}",
                program_config.admin_key
            );
            println!(
                "Debt accountant key                         | {}",
                program_config.debt_accountant_key
            );
            println!(
                "Rewards accountant key                      | {}",
                program_config.rewards_accountant_key
            );
            println!(
                "Contributor manager key                     | {}",
                program_config.contributor_manager_key
            );
            println!(
                "SOL/2Z swap program ID                      | {}",
                program_config.sol_2z_swap_program_id
            );

            let distribution_parameters = &program_config.distribution_parameters;
            println!(
                "Calculation grace period                    | {:?}",
                std::time::Duration::from_secs(
                    distribution_parameters
                        .calculation_grace_period_seconds
                        .into()
                ),
            );
            println!(
                "Minimum duration to finalize rewards        | {} epoch{}",
                distribution_parameters.minimum_epoch_duration_to_finalize_rewards,
                if distribution_parameters.minimum_epoch_duration_to_finalize_rewards == 1 {
                    ""
                } else {
                    "s"
                }
            );

            let community_burn_rate_params =
                &distribution_parameters.community_burn_rate_parameters;
            let community_burn_rate_mode = community_burn_rate_params.mode();
            println!(
                "Next community burn rate                    | {:.7}% ({})",
                u32::from(community_burn_rate_params.next_burn_rate().unwrap()) as f64
                    / 10_000_000.0,
                community_burn_rate_mode.to_string().to_lowercase()
            );
            if community_burn_rate_mode != CommunityBurnRateMode::Limit {
                println!(
                    "Community burn rate limit                   | {:.7}%",
                    u32::from(community_burn_rate_params.limit) as f64 / 10_000_000.0
                );
            }
            match community_burn_rate_mode {
                CommunityBurnRateMode::Static => {
                    println!(
                        "Community burn rate increases after         | {} epoch{}",
                        community_burn_rate_params.dz_epochs_to_increasing,
                        if community_burn_rate_params.dz_epochs_to_increasing == 1 {
                            ""
                        } else {
                            "s"
                        }
                    );
                    println!(
                        "Community burn rate limit reached after     | {} epoch{}",
                        community_burn_rate_params.dz_epochs_to_limit,
                        if community_burn_rate_params.dz_epochs_to_limit == 1 {
                            ""
                        } else {
                            "s"
                        }
                    );
                }
                CommunityBurnRateMode::Increasing => {
                    println!(
                        "Community burn rate limit reached after     | {} epoch{}",
                        community_burn_rate_params.dz_epochs_to_limit,
                        if community_burn_rate_params.dz_epochs_to_limit == 1 {
                            ""
                        } else {
                            "s"
                        }
                    );
                }
                CommunityBurnRateMode::Limit => {}
            }

            let solana_validator_fee_params =
                &distribution_parameters.solana_validator_fee_parameters;
            println!(
                "Solana validator base block rewards fee     | {:.2}%",
                u16::from(solana_validator_fee_params.base_block_rewards_pct) as f64 / 100.0
            );
            println!(
                "Solana validator priority block rewards fee | {:.2}%",
                u16::from(solana_validator_fee_params.priority_block_rewards_pct) as f64 / 100.0
            );
            println!(
                "Solana validator inflation rewards fee      | {:.2}%",
                u16::from(solana_validator_fee_params.inflation_rewards_pct) as f64 / 100.0
            );
            println!(
                "Solana validator Jito tips fee              | {:.2}%",
                u16::from(solana_validator_fee_params.jito_tips_pct) as f64 / 100.0
            );
            println!(
                "Solana validator fixed SOL fee              | {:.9} SOL",
                solana_validator_fee_params.fixed_sol_amount as f64 * 1e-9
            );

            let relay_parameters = &program_config.relay_parameters;
            println!(
                "Distribute rewards relay amount             | {:.9} SOL",
                relay_parameters.distribute_rewards_lamports as f64 * 1e-9
            );
            println!();
        } else if solana_validator_fees {
            let (program_config_key, program_config) =
                super::try_fetch_program_config(&connection).await?;

            println!("Program config: {program_config_key}");
            println!();

            match program_config.checked_solana_validator_fee_parameters() {
                Some(fee_params) => {
                    println!("Solana validator fee   | Value");
                    println!("-----------------------+--------------------");
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
                }
                None => {
                    println!("... Solana validator fee parameters not configured yet");
                }
            }
            println!();
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

        if let Some(node_id) = solana_validator_deposit {
            let (deposit_key, deposit, deposit_balance) =
                super::fetch_solana_validator_deposit(&connection, &node_id).await;

            println!("Solana validator deposit: {deposit_key}");
            println!();

            match deposit {
                Some(deposit) => {
                    println!("Node ID: {}", deposit.node_id);
                    println!("Balance: {:.9} SOL", deposit_balance as f64 * 1e-9);
                }
                None => {
                    println!("... not found");
                }
            }
            println!();
        }

        Ok(())
    }
}
