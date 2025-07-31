use crate::{
    filters::{build_account_type_filter, build_epoch_filter},
    settings::Settings,
    types::DZServiceabilityData,
};
use anyhow::{Context, Result};
use backon::Retryable;
use doublezero_serviceability::state::{
    accounttype::AccountType, contributor::Contributor, device::Device, exchange::Exchange,
    link::Link, location::Location, multicastgroup::MulticastGroup, user::User,
};
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    client_error::ClientError as SolanaClientError,
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
};
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey};
use std::{str::FromStr, time::Duration};
use tracing::{debug, info, warn};

/// Account types that we actually process in the rewards calculator
/// We ignore GlobalState, Config, ProgramConfig, and Contributor
const PROCESSED_ACCOUNT_TYPES: &[AccountType] = &[
    AccountType::Location,
    AccountType::Exchange,
    AccountType::Device,
    AccountType::Link,
    AccountType::User,
    AccountType::MulticastGroup,
    AccountType::Contributor,
];

/// Fetch all network serviceability data at a given timestamp
/// For now, we fetch the latest state (no historical slot lookup)
pub async fn fetch(
    rpc_client: &RpcClient,
    settings: &Settings,
    timestamp_us: u64,
) -> Result<DZServiceabilityData> {
    let program_id = &settings.ingestor.programs.serviceability_program_id;

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
        filters: None,
        account_config: RpcAccountInfoConfig {
            encoding: Some(UiAccountEncoding::Base64Zstd),
            commitment: Some(CommitmentConfig::finalized()),
            // For now, we don't use min_context_slot since we don't have timestamp->slot conversion
            ..RpcAccountInfoConfig::default()
        },
        ..RpcProgramAccountsConfig::default()
    };

    let accounts = (|| async {
        rpc_client
            .get_program_accounts_with_config(&program_pubkey, config.clone())
            .await
    })
    .retry(&settings.backoff())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    info!(
        "Found {} serviceability accounts to process",
        accounts.len()
    );

    // Process accounts by type
    let mut serviceability_data = DZServiceabilityData::default();
    let mut total_processed = 0;

    for (pubkey, account) in &accounts {
        if account.data.is_empty() {
            continue;
        }

        // Determine account type from first byte (discriminator)
        let account_type = AccountType::from(account.data[0]);

        match account_type {
            AccountType::Location => {
                let location = Location::from(&account.data[..]);
                serviceability_data.locations.insert(*pubkey, location);
                total_processed += 1;
            }
            AccountType::Exchange => {
                let exchange = Exchange::from(&account.data[..]);
                serviceability_data.exchanges.insert(*pubkey, exchange);
                total_processed += 1;
            }
            AccountType::Device => {
                let device = Device::from(&account.data[..]);
                serviceability_data.devices.insert(*pubkey, device);
                total_processed += 1;
            }
            AccountType::Link => {
                let link = Link::from(&account.data[..]);
                serviceability_data.links.insert(*pubkey, link);
                total_processed += 1;
            }
            AccountType::User => {
                let user = User::from(&account.data[..]);
                serviceability_data.users.insert(*pubkey, user);
                total_processed += 1;
            }
            AccountType::MulticastGroup => {
                let group = MulticastGroup::from(&account.data[..]);
                serviceability_data.multicast_groups.insert(*pubkey, group);
                total_processed += 1;
            }
            AccountType::Contributor => {
                let contributor = Contributor::from(&account.data[..]);
                serviceability_data
                    .contributors
                    .insert(*pubkey, contributor);
                total_processed += 1;
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
        "Processed {} serviceability accounts: {} contributors, {} locations, {} exchanges, {} devices, {} links, {} users, {} multicast groups",
        total_processed,
        serviceability_data.contributors.len(),
        serviceability_data.locations.len(),
        serviceability_data.exchanges.len(),
        serviceability_data.devices.len(),
        serviceability_data.links.len(),
        serviceability_data.users.len(),
        serviceability_data.multicast_groups.len(),
    );

    Ok(serviceability_data)
}

/// Fetch serviceability data by account type using RPC filters
pub async fn fetch_by_type(
    rpc_client: &RpcClient,
    settings: &Settings,
    account_type: AccountType,
    epoch: Option<u64>,
) -> Result<Vec<(Pubkey, Vec<u8>)>> {
    let program_id = &settings.ingestor.programs.serviceability_program_id;
    let program_pubkey = Pubkey::from_str(program_id)
        .with_context(|| format!("Invalid serviceability program ID: {program_id}"))?;

    info!(
        "Fetching {} accounts from program {} {}",
        account_type,
        program_id,
        if let Some(epoch) = epoch {
            format!("for epoch {epoch}")
        } else {
            "without epoch filter".to_string()
        }
    );

    let filters = if let Some(epoch) = epoch {
        // Use 9-byte filter: account type (1 byte) + epoch (8 bytes)
        build_epoch_filter(account_type as u8, epoch)
    } else {
        // Fall back to account type only filter
        build_account_type_filter(account_type as u8)
    };

    let config = RpcProgramAccountsConfig {
        filters: Some(filters),
        account_config: RpcAccountInfoConfig {
            encoding: Some(UiAccountEncoding::Base64Zstd),
            commitment: Some(CommitmentConfig::finalized()),
            ..RpcAccountInfoConfig::default()
        },
        ..RpcProgramAccountsConfig::default()
    };

    let accounts = (|| async {
        rpc_client
            .get_program_accounts_with_config(&program_pubkey, config.clone())
            .await
    })
    .retry(&settings.backoff())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;

    info!("Found {} {} accounts", accounts.len(), account_type);

    // Convert from Vec<(Pubkey, Account)> to Vec<(Pubkey, Vec<u8>)>
    let accounts_with_data: Vec<(Pubkey, Vec<u8>)> = accounts
        .into_iter()
        .map(|(pubkey, account)| (pubkey, account.data))
        .collect();

    Ok(accounts_with_data)
}

/// Fetch all serviceability data using per-type RPC filters for efficiency
pub async fn fetch_filtered(
    rpc_client: &RpcClient,
    settings: &Settings,
    timestamp_us: u64,
    epoch: Option<u64>,
) -> Result<DZServiceabilityData> {
    info!(
        "Fetching serviceability network data at timestamp {} with filtered approach{}",
        timestamp_us,
        if let Some(epoch) = epoch {
            format!(" for epoch {epoch}")
        } else {
            "".to_string()
        }
    );

    let mut serviceability_data = DZServiceabilityData::default();
    let mut total_processed = 0;
    let mut total_errors = 0;

    // Fetch each account type separately with RPC filtering
    for account_type in PROCESSED_ACCOUNT_TYPES {
        match fetch_by_type(rpc_client, settings, *account_type, epoch).await {
            Ok(accounts) => {
                info!("Processing {} {} accounts", accounts.len(), account_type);

                for (pubkey, account_data) in accounts {
                    if account_data.is_empty() {
                        continue;
                    }

                    match account_type {
                        AccountType::Location => {
                            let location = Location::from(&account_data[..]);
                            serviceability_data.locations.insert(pubkey, location);
                            total_processed += 1;
                        }
                        AccountType::Exchange => {
                            let exchange = Exchange::from(&account_data[..]);
                            serviceability_data.exchanges.insert(pubkey, exchange);
                            total_processed += 1;
                        }
                        AccountType::Device => {
                            let device = Device::from(&account_data[..]);
                            serviceability_data.devices.insert(pubkey, device);
                            total_processed += 1;
                        }
                        AccountType::Link => {
                            let link = Link::from(&account_data[..]);
                            serviceability_data.links.insert(pubkey, link);
                            total_processed += 1;
                        }
                        AccountType::User => {
                            let user = User::from(&account_data[..]);
                            serviceability_data.users.insert(pubkey, user);
                            total_processed += 1;
                        }
                        AccountType::MulticastGroup => {
                            let group = MulticastGroup::from(&account_data[..]);
                            serviceability_data.multicast_groups.insert(pubkey, group);
                            total_processed += 1;
                        }
                        AccountType::Contributor => {
                            let contributor = Contributor::from(&account_data[..]);
                            serviceability_data.contributors.insert(pubkey, contributor);
                            total_processed += 1;
                        }
                        _ => {
                            warn!(
                                "Unexpected account type {:?} in processed list",
                                account_type
                            );
                        }
                    }
                }
            }
            Err(e) => {
                warn!("Failed to fetch {} accounts: {}", account_type, e);
                total_errors += 1;
            }
        }
    }

    info!(
        "Processed {}, Errors: {}; serviceability accounts: {} contributors, {} locations, {} exchanges, {} devices, {} links, {} users, {} multicast groups",
        total_processed,
        total_errors,
        serviceability_data.contributors.len(),
        serviceability_data.locations.len(),
        serviceability_data.exchanges.len(),
        serviceability_data.devices.len(),
        serviceability_data.links.len(),
        serviceability_data.users.len(),
        serviceability_data.multicast_groups.len(),
    );

    Ok(serviceability_data)
}
