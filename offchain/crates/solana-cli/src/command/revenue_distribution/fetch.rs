use anyhow::{Result, anyhow};
use clap::{Args, Subcommand};
use doublezero_program_tools::PrecomputedDiscriminator;
use doublezero_program_tools::zero_copy;
use doublezero_revenue_distribution::state::{CommunityBurnRateMode, Journal};
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig};
use solana_client::rpc_filter::{Memcmp, RpcFilterType};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Subcommand)]
pub enum FetchSubcommand {
    /// Show program config and parameters
    Config(SolanaConnectionOptions),

    /// Print the on-chain journal account (debug format for now)
    Journal(SolanaConnectionOptions),

    /// Show configured Solana validator fee parameters (if any)
    ValidatorFees(SolanaConnectionOptions),

    /// List Solana validator deposit accounts with their balances with optional node ID filter
    ValidatorDeposits {
        #[arg(long = "node-id", short = 'n', value_name = "PUBKEY")]
        node_id: Option<Pubkey>,
        #[command(flatten)]
        connection_options: SolanaConnectionOptions,
    },
}

#[derive(Debug, Args)]
pub struct FetchCommand {
    #[command(subcommand)]
    cmd: FetchSubcommand,
}

impl FetchCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self.cmd {
            FetchSubcommand::Config(connection_options) => {
                let connection = SolanaConnection::try_from(connection_options)?;
                let (program_config_key, program_config) =
                    super::try_fetch_program_config(&connection).await?;

                println!("Program config: {program_config_key}\n");
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
                        u64::from(distribution_parameters.calculation_grace_period_minutes) * 60
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
                    u16::from(solana_validator_fee_params.priority_block_rewards_pct) as f64
                        / 100.0
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
            }

            FetchSubcommand::ValidatorFees(connection_options) => {
                let connection = SolanaConnection::try_from(connection_options)?;
                let (program_config_key, program_config) =
                    super::try_fetch_program_config(&connection).await?;

                println!("Program config: {program_config_key}\n");

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
                    None => println!("... Solana validator fee parameters not configured yet"),
                }
                println!();
            }

            FetchSubcommand::Journal(connection_options) => {
                let connection = SolanaConnection::try_from(connection_options)?;
                let journal_key = Journal::find_address().0;
                let journal_info = connection.get_account(&journal_key).await?;
                let (journal, _) =
                    zero_copy::checked_from_bytes_with_discriminator::<Journal>(&journal_info.data)
                        .ok_or(anyhow!("Failed to deserialize journal"))?;
                println!("Journal: {journal:?}");
            }

            FetchSubcommand::ValidatorDeposits {
                node_id,
                connection_options,
            } => {
                let connection = SolanaConnection::try_from(connection_options)?;
                let config = RpcProgramAccountsConfig {
                    filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                        0,
                        doublezero_revenue_distribution::state::SolanaValidatorDeposit::discriminator_slice().to_vec(),
                    ))]),
                    account_config: RpcAccountInfoConfig {
                        encoding: Some(UiAccountEncoding::Base64),
                        ..Default::default()
                    },
                    ..Default::default()
                };

                let outputs = if let Some(node_id) = node_id {
                    let (deposit_key, _deposit, deposit_balance) =
                        super::fetch_solana_validator_deposit(&connection, &node_id).await;
                    vec![(deposit_key, node_id, deposit_balance)]
                } else {
                    let accounts = connection
                        .get_program_accounts_with_config(
                            &doublezero_revenue_distribution::ID,
                            config,
                        )
                        .await?;

                    let rent_exemption = connection
                        .rpc_client
                        .get_minimum_balance_for_rent_exemption(zero_copy::data_end::<
                            doublezero_revenue_distribution::state::SolanaValidatorDeposit,
                        >())
                        .await?;

                    let mut outputs = Vec::new();
                    for (pubkey, account) in accounts {
                        let balance = account.lamports.saturating_sub(rent_exemption);
                        let (account, _) = zero_copy::checked_from_bytes_with_discriminator::<
                            doublezero_revenue_distribution::state::SolanaValidatorDeposit,
                        >(&account.data)
                        .ok_or(anyhow!("Failed to deserialize solana validator deposit"))?;
                        outputs.push((pubkey, account.node_id, balance));
                    }

                    outputs
                };

                let mut outputs = outputs.into_iter().collect::<Vec<_>>();
                outputs.sort_by_key(|(pubkey, node_id, _)| (*node_id, *pubkey));

                println!(
                    "Solana validator deposit accounts            | Node ID                                     | Balance (SOL)"
                );
                println!(
                    "---------------------------------------------+---------------------------------------------+--------------"
                );

                for (pubkey, node_id, balance) in outputs {
                    println!("{} | {} | {:.9}", pubkey, node_id, balance as f64 * 1e-9);
                }
                println!();
            }
        }

        Ok(())
    }
}
