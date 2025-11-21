use std::time::{Duration, Instant};

use anyhow::{Context, Result, anyhow, bail};
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    ID as REVENUE_DISTRIBUTION_PROGRAM_ID,
    instruction::{
        RevenueDistributionInstructionData, account::ConfigureDistributionRewardsAccounts,
    },
    state::Distribution,
    types::DoubleZeroEpoch,
};
use doublezero_solana_client_tools::rpc::try_fetch_zero_copy_data_with_commitment;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{
    message::{VersionedMessage, v0::Message},
    signature::{Keypair, Signature, Signer},
    transaction::VersionedTransaction,
};
use svm_hash::sha2::Hash;
use tokio::time::sleep;
use tracing::{info, warn};

/// Check if calculation is allowed for a given distribution based on current block timestamp
async fn check_calculation_allowed(
    rpc_client: &RpcClient,
    distribution: &Distribution,
) -> Result<bool> {
    // Get current slot and its block time from Solana
    let current_slot = rpc_client.get_slot().await?;
    let current_timestamp = rpc_client.get_block_time(current_slot).await?;

    let is_allowed = distribution
        .checked_calculation_allowed_timestamp()
        .is_some_and(|allowed_timestamp| current_timestamp >= allowed_timestamp);

    Ok(is_allowed)
}

/// Wait for the grace period to expire before posting merkle root
/// Returns the Distribution account data for reuse
async fn wait_for_grace_period(
    rpc_client: &RpcClient,
    epoch: u64,
    max_wait_seconds: u64,
) -> Result<Distribution> {
    let dz_epoch = DoubleZeroEpoch::new(epoch);
    let (distribution_key, _) = Distribution::find_address(dz_epoch);

    info!(
        "Checking grace period for epoch {} at address {}",
        epoch, distribution_key
    );

    // Fetch Distribution account
    let distribution = try_fetch_zero_copy_data_with_commitment::<Distribution>(
        rpc_client,
        &distribution_key,
        rpc_client.commitment(),
    )
    .await
    .with_context(|| {
        format!(
            "Distribution account for epoch {} does not exist at {}. \
                    It needs to be initialized by validator-debt crate first.",
            epoch, distribution_key
        )
    })?;

    // Poll until grace period is satisfied
    let max_wait = Duration::from_secs(max_wait_seconds);
    let poll_interval = Duration::from_secs(60);
    let start = Instant::now();

    loop {
        if check_calculation_allowed(rpc_client, &distribution).await? {
            info!(
                "Grace period satisfied for epoch {} after waiting {:?}",
                epoch,
                start.elapsed()
            );
            return Ok(*distribution);
        }

        if start.elapsed() >= max_wait {
            bail!(
                "Exceeded max wait time ({:?}) for grace period on epoch {}",
                max_wait,
                epoch
            );
        }

        if let Some(allowed_timestamp) = distribution.checked_calculation_allowed_timestamp() {
            // Get current Solana block time
            let current_slot = rpc_client.get_slot().await?;
            let current_timestamp = rpc_client.get_block_time(current_slot).await?;
            let wait_seconds = allowed_timestamp - current_timestamp;

            warn!(
                "Calculation grace period not satisfied for epoch {}. Waiting approximately {} more seconds (elapsed: {:?})",
                epoch,
                wait_seconds.max(0),
                start.elapsed()
            );
        }

        sleep(poll_interval).await;
    }
}

/// Post the contributor rewards merkle root to the revenue distribution program
pub async fn post_rewards_merkle_root(
    rpc_client: &RpcClient,
    payer_signer: &Keypair,
    epoch: u64,
    total_contributors: u32,
    merkle_root: Hash,
    max_wait_seconds: u64,
) -> Result<Signature> {
    info!(
        "Posting merkle root for epoch {} with {} contributors to program {}",
        epoch, total_contributors, REVENUE_DISTRIBUTION_PROGRAM_ID
    );

    // Wait for grace period and get Distribution account (validates existence and grace period)
    let _distribution = wait_for_grace_period(rpc_client, epoch, max_wait_seconds).await?;

    let dz_epoch = DoubleZeroEpoch::new(epoch);

    // Build the ConfigureDistributionRewards instruction with the helper
    let ix_data = RevenueDistributionInstructionData::ConfigureDistributionRewards {
        total_contributors,
        merkle_root,
    };

    let accounts = ConfigureDistributionRewardsAccounts::new(&payer_signer.pubkey(), dz_epoch);

    let ix = try_build_instruction(&REVENUE_DISTRIBUTION_PROGRAM_ID, accounts, &ix_data)?;

    // Build versioned transaction
    let recent_blockhash = rpc_client.get_latest_blockhash().await?;

    let message = Message::try_compile(&payer_signer.pubkey(), &[ix], &[], recent_blockhash)?;

    let transaction =
        VersionedTransaction::try_new(VersionedMessage::V0(message), &[payer_signer])?;

    // Send transaction
    let signature = rpc_client
        .send_and_confirm_transaction(&transaction)
        .await
        .map_err(|e| anyhow!("Failed to post merkle root for epoch {epoch}: {e}"))?;

    info!(
        "Successfully posted merkle root for epoch {} with signature: {}",
        epoch, signature
    );

    Ok(signature)
}
