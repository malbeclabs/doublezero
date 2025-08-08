mod common;

//

use doublezero_revenue_distribution::{
    instruction::{
        DistributionMerkleRootKind, DistributionPaymentsConfiguration, ProgramConfiguration,
        ProgramFlagConfiguration,
    },
    types::{DoubleZeroEpoch, SolanaValidatorPayment},
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::signature::{Keypair, Signer};
use svm_hash::merkle::{merkle_root_from_byte_ref_leaves, MerkleProof};

//
// Verify distribution merkle root.
//

#[tokio::test]
async fn test_verify_distribution_merkle_root() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    let payments_accountant_signer = Keypair::new();
    let rewards_accountant_signer = Keypair::new();
    let solana_validator_base_block_rewards_fee = 500; // 5%.

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Relay settings.
    let contributor_reward_claim_relay_lamports = 10_000;

    // Distribution.

    let dz_epoch = DoubleZeroEpoch::new(1);

    let payments_data = (0..69)
        .map(|i| SolanaValidatorPayment {
            node_id: Pubkey::new_unique(),
            amount: 100_000_000_000 * (i + 1),
        })
        .collect::<Vec<_>>();

    let leaves = payments_data
        .iter()
        .map(|payment| borsh::to_vec(&payment).unwrap())
        .collect::<Vec<_>>();

    let merkle_root =
        merkle_root_from_byte_ref_leaves(&leaves, Some(SolanaValidatorPayment::LEAF_PREFIX))
            .unwrap();

    let total_lamports_owed = payments_data.iter().map(|payment| payment.amount).sum();

    test_setup
        .initialize_program()
        .await
        .unwrap()
        .initialize_journal()
        .await
        .unwrap()
        .set_admin(&admin_signer.pubkey())
        .await
        .unwrap()
        .configure_program(
            &admin_signer,
            [
                ProgramConfiguration::PaymentsAccountant(payments_accountant_signer.pubkey()),
                ProgramConfiguration::RewardsAccountant(rewards_accountant_signer.pubkey()),
                ProgramConfiguration::SolanaValidatorFeeParameters {
                    base_block_rewards: solana_validator_base_block_rewards_fee,
                    priority_block_rewards: 0,
                    inflation_rewards: 0,
                    jito_tips: 0,
                    _unused: [0; 32],
                },
                ProgramConfiguration::CommunityBurnRateParameters {
                    limit: cbr_limit,
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
                },
                ProgramConfiguration::ContributorRewardClaimLamports(
                    contributor_reward_claim_relay_lamports,
                ),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .initialize_distribution(&payments_accountant_signer)
        .await
        .unwrap()
        .initialize_distribution(&payments_accountant_signer)
        .await
        .unwrap()
        .configure_distribution_payments(
            dz_epoch,
            &payments_accountant_signer,
            [
                DistributionPaymentsConfiguration::UpdateSolanaValidatorPayments {
                    total_lamports_owed,
                    merkle_root,
                },
            ],
        )
        .await
        .unwrap();

    let kinds_and_proofs = payments_data
        .iter()
        .copied()
        .enumerate()
        .map(|(i, payment_owed)| {
            let proof = MerkleProof::from_byte_ref_leaves(
                &leaves,
                i,
                Some(SolanaValidatorPayment::LEAF_PREFIX),
            )
            .unwrap();

            (
                DistributionMerkleRootKind::SolanaValidatorPayment(payment_owed),
                proof,
            )
        })
        .collect::<Vec<_>>();

    for chunk in kinds_and_proofs.chunks(64) {
        test_setup
            .verify_distribution_merkle_root(dz_epoch, chunk.to_vec())
            .await
            .unwrap();
    }
}
