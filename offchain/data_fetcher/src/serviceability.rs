use crate::{
    rpc,
    types::{
        DZDevice, DZExchange, DZLink, DZLocation, DZMulticastGroup, DZServiceabilityData, DZUser,
    },
};
use anyhow::{Context, Result};
use doublezero_serviceability::state::{
    accounttype::AccountType, device::Device, exchange::Exchange, link::Link, location::Location,
    multicastgroup::MulticastGroup, user::User,
};
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
};
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey};
use std::str::FromStr;
use tracing::{debug, info};

/// Fetch all network serviceability data at a given timestamp
/// For now, we fetch the latest state (no historical slot lookup)
pub async fn fetch(
    rpc_client: &RpcClient,
    program_id: &str,
    timestamp_us: u64,
) -> Result<DZServiceabilityData> {
    let program_pubkey = Pubkey::from_str(program_id)
        .with_context(|| format!("Invalid serviceability program ID: {program_id}"))?;

    info!(
        "Fetching serviceability network data at timestamp {} from program {}",
        timestamp_us, program_id
    );

    // For serviceability data, we fetch all accounts without filters
    // since we need the complete network state
    // TODO: In the future, convert timestamp to slot for historical queries
    let config = RpcProgramAccountsConfig {
        filters: None, // No filters - we need all account types
        account_config: RpcAccountInfoConfig {
            encoding: Some(UiAccountEncoding::Base64Zstd),
            commitment: Some(CommitmentConfig::finalized()),
            // For now, we don't use min_context_slot since we don't have timestamp->slot conversion
            ..RpcAccountInfoConfig::default()
        },
        ..RpcProgramAccountsConfig::default()
    };

    // Fetch accounts with retry logic
    let accounts = rpc::with_retry(
        || async { rpc_client.get_program_accounts_with_config(&program_pubkey, config.clone()) },
        3,
        "get_program_accounts for serviceability",
    )
    .await?;

    info!(
        "Found {} serviceability accounts to process",
        accounts.len()
    );

    // Process accounts by type
    let mut serviceability_data = DZServiceabilityData::default();
    let mut processed_count = 0;

    // TODO: rayon?
    for (pubkey, account) in &accounts {
        if account.data.is_empty() {
            continue;
        }

        // Determine account type from first byte (discriminator)
        let account_type = AccountType::from(account.data[0]);

        match account_type {
            AccountType::Location => {
                let location = Location::from(&account.data[..]);
                serviceability_data
                    .locations
                    .push(DZLocation::from_solana(*pubkey, &location));
                processed_count += 1;
            }
            AccountType::Exchange => {
                let exchange = Exchange::from(&account.data[..]);
                serviceability_data
                    .exchanges
                    .push(DZExchange::from_solana(*pubkey, &exchange));
                processed_count += 1;
            }
            AccountType::Device => {
                let device = Device::from(&account.data[..]);
                serviceability_data
                    .devices
                    .push(DZDevice::from_solana(*pubkey, &device));
                processed_count += 1;
            }
            AccountType::Link => {
                let link = Link::from(&account.data[..]);
                serviceability_data
                    .links
                    .push(DZLink::from_solana(*pubkey, &link));
                processed_count += 1;
            }
            AccountType::User => {
                let user = User::from(&account.data[..]);
                serviceability_data
                    .users
                    .push(DZUser::from_solana(*pubkey, &user));
                processed_count += 1;
            }
            AccountType::MulticastGroup => {
                let group = MulticastGroup::from(&account.data[..]);
                serviceability_data
                    .multicast_groups
                    .push(DZMulticastGroup::from_solana(*pubkey, &group));
                processed_count += 1;
            }
            _ => {
                debug!(
                    "Unknown or unhandled account type {:?} for {} (size: {})",
                    account_type,
                    pubkey,
                    account.data.len()
                );
            }
        }
    }

    info!(
        "Processed {} serviceability accounts: {} locations, {} exchanges, {} devices, {} links, {} users, {} multicast groups",
        processed_count,
        serviceability_data.locations.len(),
        serviceability_data.exchanges.len(),
        serviceability_data.devices.len(),
        serviceability_data.links.len(),
        serviceability_data.users.len(),
        serviceability_data.multicast_groups.len(),
    );

    Ok(serviceability_data)
}
