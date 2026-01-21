use anyhow::{Context, Result, bail};
use clap::{Args, ValueEnum};
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    rpc::{SolanaConnection, SolanaConnectionOptions},
};
use doublezero_solana_sdk::{
    PrecomputedDiscriminator, environment_2z_token_mint_key,
    revenue_distribution::{self, state::ContributorRewards},
};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::pubkey::Pubkey;
use spl_associated_token_account_interface::address::get_associated_token_address_and_bump_seed;
use tabled::Tabled;

#[derive(Debug, Clone, Copy, PartialEq, Eq, ValueEnum, Default)]
pub enum ContributorRewardsViewMode {
    #[default]
    Summary,
    Recipients,
}

#[derive(Debug, Args)]
pub struct ContributorRewardsCommand {
    #[arg(long)]
    service_key: Option<Pubkey>,

    #[arg(long)]
    manager: Option<Pubkey>,

    #[arg(long, value_enum, default_value = "summary")]
    view: ContributorRewardsViewMode,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

#[derive(Debug, Tabled)]
struct ContributorRewardsSummaryRow {
    service_key: Pubkey,
    manager: String,
    blocks_protocol_management: &'static str,
    recipients_configured_count: u8,
}

#[derive(Debug, Tabled)]
struct ContributorRewardsRecipientRow {
    index: usize,
    recipient: Pubkey,
    ata: Pubkey,
    proportion: String,
}

impl ContributorRewardsCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self {
            service_key,
            manager,
            view,
            connection_options,
        } = self;

        // Validate: --service-key and --manager are mutually exclusive
        if service_key.is_some() && manager.is_some() {
            bail!("--service-key and --manager are mutually exclusive, please specify only one.");
        }

        // Validate: recipients view requires --service-key
        if view == ContributorRewardsViewMode::Recipients && service_key.is_none() {
            bail!("--view recipients requires --service-key to be specified");
        }

        let connection = SolanaConnection::from(connection_options);

        match view {
            ContributorRewardsViewMode::Summary => {
                try_print_summary_view(&connection, service_key, manager).await
            }
            ContributorRewardsViewMode::Recipients => {
                try_print_recipients_view(&connection, service_key.unwrap()).await
            }
        }
    }
}

async fn try_print_summary_view(
    connection: &SolanaConnection,
    service_key: Option<Pubkey>,
    manager_filter: Option<Pubkey>,
) -> Result<()> {
    let accounts = if let Some(service_key) = service_key {
        let (pda_key, _) = ContributorRewards::find_address(&service_key);

        match connection
            .try_fetch_zero_copy_data::<ContributorRewards>(&pda_key)
            .await
        {
            Ok(data) => vec![(pda_key, data)],
            Err(_) => {
                bail!("No contributor rewards account found for service key {service_key}");
            }
        }
    } else {
        let mut filters = vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
            0,
            ContributorRewards::discriminator_slice().to_vec(),
        ))];

        if let Some(manager) = manager_filter {
            filters.push(RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                8,
                manager.to_bytes().to_vec(),
            )));
        }

        let config = RpcProgramAccountsConfig {
            filters: Some(filters),
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                ..Default::default()
            },
            ..Default::default()
        };

        connection
            .get_program_accounts_with_config(&revenue_distribution::ID, config)
            .await?
            .into_iter()
            .filter_map(|(key, account)| {
                ZeroCopyAccountOwnedData::<ContributorRewards>::from_account(&account)
                    .map(|data| (key, data))
            })
            .collect()
    };

    if accounts.is_empty() {
        if manager_filter.is_some() {
            bail!("No contributor rewards accounts found for the specified manager");
        } else {
            bail!("No contributor rewards accounts found");
        }
    }

    let mut rows: Vec<ContributorRewardsSummaryRow> = accounts
        .iter()
        .map(|(_, data)| {
            let recipient_count = data.recipient_shares.active_iter().count() as u8;
            let manager_display = if data.rewards_manager_key == Pubkey::default() {
                String::new()
            } else {
                data.rewards_manager_key.to_string()
            };
            ContributorRewardsSummaryRow {
                service_key: data.service_key,
                manager: manager_display,
                blocks_protocol_management: if data.is_set_rewards_manager_blocked() {
                    "yes"
                } else {
                    "no"
                },
                recipients_configured_count: recipient_count,
            }
        })
        .collect();

    // Sort by service_key for consistent output
    rows.sort_by_key(|row| row.service_key.to_string());

    super::print_table(
        rows,
        super::TableOptions {
            columns_aligned_right: Some(&[2, 3]),
        },
    );

    Ok(())
}

async fn try_print_recipients_view(
    connection: &SolanaConnection,
    service_key: Pubkey,
) -> Result<()> {
    let (pda_key, _) = ContributorRewards::find_address(&service_key);

    let data = connection
        .try_fetch_zero_copy_data::<ContributorRewards>(&pda_key)
        .await
        .with_context(|| format!("Contributor rewards not found for service key {service_key}"))?;

    let network_env = connection.try_network_environment().await?;
    let dz_mint_key = environment_2z_token_mint_key(network_env);

    let rows: Vec<ContributorRewardsRecipientRow> = data
        .recipient_shares
        .active_iter()
        .enumerate()
        .map(|(index, share)| {
            let (ata, _) = get_associated_token_address_and_bump_seed(
                &share.recipient_key,
                &dz_mint_key,
                &spl_associated_token_account_interface::program::ID,
                &spl_token_interface::ID,
            );

            let proportion_pct = u16::from(share.share) as f64 / 100.0;

            ContributorRewardsRecipientRow {
                index,
                recipient: share.recipient_key,
                ata,
                proportion: format!("{:.2}%", proportion_pct),
            }
        })
        .collect();

    if rows.is_empty() {
        bail!("No recipients configured for service key {service_key}");
    }

    super::print_table(
        rows,
        super::TableOptions {
            columns_aligned_right: Some(&[0, 3]),
        },
    );

    Ok(())
}

#[cfg(test)]
mod tests {
    use doublezero_solana_client_tools::rpc::SolanaConnectionOptions;

    use super::*;

    const UNIT_SHARE16_MAX: u16 = 10_000;

    fn format_proportion(share: u16) -> String {
        format!("{:.2}%", share as f64 / 100.0)
    }

    #[test]
    fn test_proportion_formatting() {
        // 100% = UNIT_SHARE16_MAX = 10,000
        assert_eq!(format_proportion(UNIT_SHARE16_MAX), "100.00%");
        // 50% = 5,000
        assert_eq!(format_proportion(UNIT_SHARE16_MAX / 2), "50.00%");
        // 25% = 2,500
        assert_eq!(format_proportion(UNIT_SHARE16_MAX / 4), "25.00%");
        // 1% = 100
        assert_eq!(format_proportion(100), "1.00%");
        // 0.01% = 1 (minimum non-zero)
        assert_eq!(format_proportion(1), "0.01%");
        // 0% = 0
        assert_eq!(format_proportion(0), "0.00%");
    }

    #[test]
    fn test_view_mode_default() {
        assert_eq!(
            ContributorRewardsViewMode::default(),
            ContributorRewardsViewMode::Summary
        );
    }

    #[tokio::test]
    async fn test_recipients_view_requires_service_key() {
        // Construct the command with Recipients view but no service_key
        let cmd = ContributorRewardsCommand {
            service_key: None,
            manager: None,
            view: ContributorRewardsViewMode::Recipients,
            connection_options: SolanaConnectionOptions::default(),
        };

        // Call the real execute method - validation happens before any RPC calls
        let result = cmd.try_into_execute().await;

        // Must be an error
        assert!(
            result.is_err(),
            "Expected error when --service-key is missing"
        );

        let err_msg = result.unwrap_err().to_string();

        // Error message must mention both the view mode and the required flag
        assert!(
            err_msg.contains("--service-key"),
            "Error should mention --service-key, got: {err_msg}"
        );
        assert!(
            err_msg.contains("recipients"),
            "Error should mention recipients view, got: {err_msg}"
        );
    }

    #[tokio::test]
    async fn test_service_key_and_manager_mutually_exclusive() {
        let cmd = ContributorRewardsCommand {
            service_key: Some(Pubkey::new_unique()),
            manager: Some(Pubkey::new_unique()),
            view: ContributorRewardsViewMode::Summary,
            connection_options: SolanaConnectionOptions::default(),
        };

        // Call the real execute method - validation happens before any RPC calls
        let result = cmd.try_into_execute().await;

        assert!(
            result.is_err(),
            "Expected error when both --service-key and --manager are provided"
        );

        let err_msg = result.unwrap_err().to_string();

        assert!(
            err_msg.contains("--service-key") && err_msg.contains("--manager"),
            "Error should mention both flags, got: {err_msg}"
        );
        assert!(
            err_msg.contains("mutually exclusive"),
            "Error should mention mutual exclusivity, got: {err_msg}"
        );
    }
}
