use std::{collections::BTreeMap, time::Instant};

use anyhow::{Context, Result};
use doublezero_solana_sdk::shred_subscription::{
    ID as SHRED_SUBSCRIPTION_PROGRAM_ID,
    state::{METRO_HISTORY_DISCRIMINATOR, parse_metro_history},
};
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey};
use tracing::{debug, info, warn};

/// Fetch metro prices from the shred subscription program's MetroHistory accounts.
///
/// Returns a map of exchange pubkey to the current USDC price in whole dollars.
pub async fn fetch_metro_prices(rpc_client: &RpcClient) -> Result<BTreeMap<Pubkey, u16>> {
    let discriminator_bytes = borsh::to_vec(&METRO_HISTORY_DISCRIMINATOR)
        .context("serializing MetroHistory discriminator")?;
    let filters = vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
        0,
        discriminator_bytes,
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

    let program_id = *SHRED_SUBSCRIPTION_PROGRAM_ID;
    let start = Instant::now();
    let accounts = rpc_client
        .get_program_accounts_with_config(&program_id, config)
        .await
        .context("Failed to fetch MetroHistory accounts")?;
    debug!("Fetching MetroHistory accounts took: {:?}", start.elapsed());

    let mut metro_prices = BTreeMap::new();
    let mut errors = 0;

    for (pubkey, account) in &accounts {
        let Some(info) = parse_metro_history(&account.data) else {
            warn!(
                "Failed to parse MetroHistory account {} ({} bytes)",
                pubkey,
                account.data.len()
            );
            errors += 1;
            continue;
        };

        if info.current_usdc_price > 0 {
            metro_prices.insert(info.exchange_key, info.current_usdc_price);
        } else {
            debug!(
                "MetroHistory {} (exchange {}) has zero price, skipping",
                pubkey, info.exchange_key
            );
        }
    }

    info!(
        "Fetched {} MetroHistory accounts, {} metro prices extracted, {} errors",
        accounts.len(),
        metro_prices.len(),
        errors,
    );

    Ok(metro_prices)
}
