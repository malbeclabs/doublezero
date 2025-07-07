use crate::rpc;
use anyhow::{Context, Result};
use doublezero_serviceability::state::{
    accounttype::AccountType, device::Device, exchange::Exchange, link::Link, location::Location,
    multicastgroup::MulticastGroup, user::User,
};
use metrics_processor::engine::types::{
    DbDevice, DbExchange, DbLink, DbLocation, DbMulticastGroup, DbUser, NetworkData,
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
pub async fn fetch_network_data(
    rpc_client: &RpcClient,
    program_id: &str,
    timestamp_us: u64,
) -> Result<NetworkData> {
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
    let mut network_data = NetworkData::default();
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
                network_data
                    .locations
                    .push(DbLocation::from_solana(*pubkey, &location));
                processed_count += 1;
            }
            AccountType::Exchange => {
                let exchange = Exchange::from(&account.data[..]);
                network_data
                    .exchanges
                    .push(DbExchange::from_solana(*pubkey, &exchange));
                processed_count += 1;
            }
            AccountType::Device => {
                let device = Device::from(&account.data[..]);
                network_data
                    .devices
                    .push(DbDevice::from_solana(*pubkey, &device));
                processed_count += 1;
            }
            AccountType::Link => {
                let link = Link::from(&account.data[..]);
                network_data.links.push(DbLink::from_solana(*pubkey, &link));
                processed_count += 1;
            }
            AccountType::User => {
                let user = User::from(&account.data[..]);
                network_data.users.push(DbUser::from_solana(*pubkey, &user));
                processed_count += 1;
            }
            AccountType::MulticastGroup => {
                let group = MulticastGroup::from(&account.data[..]);
                network_data
                    .multicast_groups
                    .push(DbMulticastGroup::from_solana(*pubkey, &group));
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
        network_data.locations.len(),
        network_data.exchanges.len(),
        network_data.devices.len(),
        network_data.links.len(),
        network_data.users.len(),
        network_data.multicast_groups.len(),
    );

    Ok(network_data)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_network_data_default() {
        let network_data = NetworkData::default();

        assert_eq!(network_data.locations.len(), 0);
        assert_eq!(network_data.exchanges.len(), 0);
        assert_eq!(network_data.devices.len(), 0);
        assert_eq!(network_data.links.len(), 0);
        assert_eq!(network_data.users.len(), 0);
        assert_eq!(network_data.multicast_groups.len(), 0);
    }
}
