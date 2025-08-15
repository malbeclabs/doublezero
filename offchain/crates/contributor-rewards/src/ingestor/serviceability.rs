use crate::{ingestor::types::DZServiceabilityData, settings::Settings};
use anyhow::{Context, Result};
use backon::{ExponentialBuilder, Retryable};
use doublezero_serviceability::state::{
    accounttype::AccountType, contributor::Contributor, device::Device, exchange::Exchange,
    link::Link, location::Location, multicastgroup::MulticastGroup, user::User,
};
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    client_error::ClientError as SolanaClientError,
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey};
use std::{
    str::FromStr,
    time::{Duration, Instant},
};
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

pub async fn fetch(rpc_client: &RpcClient, settings: &Settings) -> Result<DZServiceabilityData> {
    // NOTE: This fetches current serviceability state only
    // Historical state is not available as serviceability accounts
    // don't have timestamp/epoch fields and updates overwrite data.
    // This creates a temporal mismatch with historical telemetry data.
    let mut serviceability_data = DZServiceabilityData::default();
    let mut total_processed = 0;
    let mut total_errors = 0;

    // Fetch each account type separately with RPC filtering
    for account_type in PROCESSED_ACCOUNT_TYPES {
        match fetch_by_type(rpc_client, settings, *account_type).await {
            Err(e) => {
                warn!("Failed to fetch {} accounts: {}", account_type, e);
                total_errors += 1;
            }
            Ok(accounts) => {
                debug!("Processing {} {} accounts", accounts.len(), account_type);

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
        }
    }

    info!(
        "Processed {} serviceability accounts, contributors={}, locations={}, exchanges={}, devices={}, links={}, users={}, mcast_groups={}. Errors={}",
        total_processed,
        serviceability_data.contributors.len(),
        serviceability_data.locations.len(),
        serviceability_data.exchanges.len(),
        serviceability_data.devices.len(),
        serviceability_data.links.len(),
        serviceability_data.users.len(),
        serviceability_data.multicast_groups.len(),
        total_errors,
    );

    Ok(serviceability_data)
}

/// Fetch serviceability data by account type using RPC filters
async fn fetch_by_type(
    rpc_client: &RpcClient,
    settings: &Settings,
    account_type: AccountType,
) -> Result<Vec<(Pubkey, Vec<u8>)>> {
    let program_id = &settings.programs.serviceability_program_id;
    let program_pubkey = Pubkey::from_str(program_id)
        .with_context(|| format!("Invalid serviceability program ID: {program_id}"))?;

    let filters = vec![RpcFilterType::Memcmp(Memcmp::new_base58_encoded(
        0,
        &[account_type as u8],
    ))];

    let config = RpcProgramAccountsConfig {
        filters: Some(filters),
        account_config: RpcAccountInfoConfig {
            encoding: Some(UiAccountEncoding::Base64Zstd),
            commitment: Some(CommitmentConfig::finalized()),
            ..RpcAccountInfoConfig::default()
        },
        ..RpcProgramAccountsConfig::default()
    };

    let start = Instant::now();
    let accounts = (|| async {
        rpc_client
            .get_program_accounts_with_config(&program_pubkey, config.clone())
            .await
    })
    .retry(&ExponentialBuilder::default().with_jitter())
    .notify(|err: &SolanaClientError, dur: Duration| {
        info!("retrying error: {:?} with sleeping {:?}", err, dur)
    })
    .await?;
    debug!(
        "Fetching serviceability account took: {:?}",
        start.elapsed()
    );

    debug!("Found {} {} accounts", accounts.len(), account_type);
    // Convert from Vec<(Pubkey, Account)> to Vec<(Pubkey, Vec<u8>)>
    let accounts_with_data: Vec<(Pubkey, Vec<u8>)> = accounts
        .into_iter()
        .map(|(pubkey, account)| (pubkey, account.data))
        .collect();

    Ok(accounts_with_data)
}
