use anyhow::{Result, anyhow, bail};
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    ID as REVENUE_DISTRIBUTION_PROGRAM_ID,
    instruction::{
        RevenueDistributionInstructionData, account::ConfigureDistributionRewardsAccounts,
    },
    state::Distribution,
    types::DoubleZeroEpoch,
};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{
    commitment_config::CommitmentConfig,
    message::{VersionedMessage, v0::Message},
    signature::{Keypair, Signer},
    transaction::VersionedTransaction,
};
use svm_hash::sha2::Hash;
use tracing::info;

/// Post the contributor rewards merkle root to the revenue distribution program
pub async fn post_rewards_merkle_root(
    rpc_client: &RpcClient,
    payer_signer: &Keypair,
    epoch: u64,
    total_contributors: u32,
    merkle_root: Hash,
) -> Result<()> {
    info!(
        "Posting merkle root for epoch {} with {} contributors to program {}",
        epoch, total_contributors, REVENUE_DISTRIBUTION_PROGRAM_ID
    );

    // Derive the Distribution account PDA
    let dz_epoch = DoubleZeroEpoch::new(epoch);
    let (distribution_pubkey, _) = Distribution::find_address(dz_epoch);

    // Check if Distribution account exists
    let distribution_account = rpc_client
        .get_account_with_commitment(&distribution_pubkey, CommitmentConfig::confirmed())
        .await?;

    if distribution_account.value.is_none() {
        bail!(
            "Distribution account for epoch {} does not exist at {}. \
            It should be initialized by validator-revenue crate first.",
            epoch,
            distribution_pubkey
        );
    }

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
    rpc_client
        .send_and_confirm_transaction(&transaction)
        .await
        .map(|signature| {
            info!(
                "Successfully posted merkle root for epoch {} with signature: {}",
                epoch, signature
            );
        })
        .map_err(|e| anyhow!("Failed to post merkle root for epoch {}: {}", epoch, e))
}
