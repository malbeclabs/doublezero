use std::{
    str::FromStr,
    time::{Duration, Instant},
};

use anyhow::{Context, Result, bail};
use backon::{ExponentialBuilder, Retryable};
use doublezero_serviceability::state::{
    accesspass::AccessPass, accounttype::AccountType, contributor::Contributor, device::Device,
    exchange::Exchange, link::Link, location::Location, multicastgroup::MulticastGroup, user::User,
};
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    client_error::ClientError as SolanaClientError,
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::pubkey::Pubkey;
use tracing::{debug, info, warn};

use crate::{ingestor::types::DZServiceabilityData, settings::Settings};

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
    AccountType::AccessPass,
];

pub async fn fetch(rpc_client: &RpcClient, settings: &Settings) -> Result<DZServiceabilityData> {
    // NOTE: This fetches current serviceability state only
    // Historical state is not available as serviceability accounts
    // don't have timestamp/epoch fields and updates overwrite data.
    // This creates a temporal mismatch with historical telemetry data.
    let mut serviceability_data = DZServiceabilityData::default();
    let mut total_processed = 0;
    let mut total_fetch_errors = 0;
    let mut total_decode_failures = 0;
    // Reward-bearing types whose decode failures must fail the epoch, paired with
    // the offending pubkeys. Collected across the full sweep so every bad account
    // is warned before we bail (see the policy note below).
    let mut fatal_decode_failures = Vec::new();

    // Fetch each account type separately with RPC filtering
    for account_type in PROCESSED_ACCOUNT_TYPES {
        match fetch_by_type(rpc_client, settings, *account_type).await {
            Err(e) => {
                warn!("Failed to fetch {} accounts: {}", account_type, e);
                total_fetch_errors += 1;
            }
            Ok(accounts) => {
                debug!("Processing {} {} accounts", accounts.len(), account_type);

                let (processed, failed_pubkeys) =
                    decode_accounts(&mut serviceability_data, *account_type, accounts);
                total_processed += processed;
                total_decode_failures += failed_pubkeys.len();

                // Policy split: AccessPass is reward-neutral, so a decode failure
                // there is skipped (already warned + counted). Every other type is
                // reward-bearing — a partial snapshot would feed Shapley a silently
                // shrunk graph and freeze a skewed merkle permanently, so those
                // decode failures fail the epoch loudly instead.
                if !failed_pubkeys.is_empty() && *account_type != AccountType::AccessPass {
                    fatal_decode_failures.push((*account_type, failed_pubkeys));
                }
            }
        }
    }

    info!(
        "Processed {} serviceability accounts, contributors={}, locations={}, exchanges={}, devices={}, links={}, users={}, mcast_groups={}, access_passes={}. Errors={}, DecodeErrors={}",
        total_processed,
        serviceability_data.contributors.len(),
        serviceability_data.locations.len(),
        serviceability_data.exchanges.len(),
        serviceability_data.devices.len(),
        serviceability_data.links.len(),
        serviceability_data.users.len(),
        serviceability_data.multicast_groups.len(),
        serviceability_data.access_passes.len(),
        total_fetch_errors,
        total_decode_failures,
    );

    if !fatal_decode_failures.is_empty() {
        let affected = fatal_decode_failures.len();
        let detail = fatal_decode_failures
            .iter()
            .map(|(account_type, pubkeys)| {
                let count = pubkeys.len();
                let pubkeys = pubkeys
                    .iter()
                    .map(|pubkey| pubkey.to_string())
                    .collect::<Vec<_>>()
                    .join(", ");
                format!("{account_type} ({count}): {pubkeys}")
            })
            .collect::<Vec<_>>()
            .join("; ");
        bail!(
            "Aborting serviceability snapshot: {affected} reward-bearing account type(s) had undecodable accounts: {detail}"
        );
    }

    Ok(serviceability_data)
}

// Decode every fetched account of one type into `serviceability_data`, warning,
// counting, and skipping each account that fails to decode. Returns the number
// of accounts stored and the pubkeys that failed to decode; the caller decides
// whether those failures are tolerable (AccessPass) or must fail the epoch.
// Kept separate from the RPC fetch so the skip-and-count policy is the tested
// unit rather than a loop re-implemented in a test.
fn decode_accounts(
    serviceability_data: &mut DZServiceabilityData,
    account_type: AccountType,
    accounts: Vec<(Pubkey, Vec<u8>)>,
) -> (usize, Vec<Pubkey>) {
    let mut processed = 0;
    let mut failed_pubkeys = Vec::new();

    for (pubkey, account_data) in accounts {
        if account_data.is_empty() {
            continue;
        }

        let decoded = match account_type {
            AccountType::Location => Location::try_from(&account_data[..]).map(|location| {
                serviceability_data.locations.insert(pubkey, location);
            }),
            AccountType::Exchange => Exchange::try_from(&account_data[..]).map(|exchange| {
                serviceability_data.exchanges.insert(pubkey, exchange);
            }),
            AccountType::Device => Device::try_from(&account_data[..]).map(|device| {
                serviceability_data.devices.insert(pubkey, device);
            }),
            AccountType::Link => Link::try_from(&account_data[..]).map(|link| {
                serviceability_data.links.insert(pubkey, link);
            }),
            AccountType::User => User::try_from(&account_data[..]).map(|user| {
                serviceability_data.users.insert(pubkey, user);
            }),
            AccountType::MulticastGroup => {
                MulticastGroup::try_from(&account_data[..]).map(|multicast_group| {
                    serviceability_data
                        .multicast_groups
                        .insert(pubkey, multicast_group);
                })
            }
            AccountType::Contributor => {
                Contributor::try_from(&account_data[..]).map(|contributor| {
                    serviceability_data.contributors.insert(pubkey, contributor);
                })
            }
            AccountType::AccessPass => AccessPass::try_from(&account_data[..]).map(|access_pass| {
                serviceability_data
                    .access_passes
                    .insert(pubkey, access_pass);
            }),
            _ => {
                warn!(
                    "Unexpected account type {:?} in processed list",
                    account_type
                );
                continue;
            }
        };

        match decoded {
            Ok(()) => processed += 1,
            Err(e) => {
                warn!(
                    "Failed to decode {} account {} ({} bytes): {}",
                    account_type,
                    pubkey,
                    account_data.len(),
                    e
                );
                metrics::counter!(
                    "doublezero_contributor_rewards_serviceability_decode_errors",
                    "account_type" => account_type.to_string(),
                )
                .increment(1);
                failed_pubkeys.push(pubkey);
            }
        }
    }

    (processed, failed_pubkeys)
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

#[cfg(test)]
mod tests {
    use doublezero_serviceability::state::location::LocationStatus;

    use super::*;

    // A mixed batch (one valid Location, one undecodable account) must store the
    // valid account, report exactly one decode failure with its pubkey, and leave
    // the garbage account out of the map.
    #[test]
    fn test_decode_accounts_stores_valid_and_reports_undecodable() {
        let mut serviceability_data = DZServiceabilityData::default();
        let valid_pubkey = Pubkey::new_unique();
        let garbage_pubkey = Pubkey::new_unique();

        let location = Location {
            account_type: AccountType::Location,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 255,
            lat: 52.37,
            lng: 4.9,
            loc_id: 42,
            status: LocationStatus::Activated,
            code: "ams".to_string(),
            name: "Amsterdam".to_string(),
            country: "NL".to_string(),
            reference_count: 0,
        };
        let accounts = vec![
            (valid_pubkey, borsh::to_vec(&location).unwrap()),
            // Leading discriminant is not `AccountType::Location`, so this fails
            // to decode.
            (garbage_pubkey, vec![0xFF, 0x00, 0x00]),
        ];

        let (processed, failed_pubkeys) =
            decode_accounts(&mut serviceability_data, AccountType::Location, accounts);

        assert_eq!(processed, 1);
        assert_eq!(failed_pubkeys, vec![garbage_pubkey]);
        assert_eq!(serviceability_data.locations.len(), 1);
        assert!(serviceability_data.locations.contains_key(&valid_pubkey));
        assert!(!serviceability_data.locations.contains_key(&garbage_pubkey));
    }
}
