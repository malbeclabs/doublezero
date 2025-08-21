mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::VerifyDistributionMerkleRootAccounts, DistributionMerkleRootKind,
        DistributionPaymentsConfiguration, ProgramConfiguration, ProgramFlagConfiguration,
        RevenueDistributionInstructionData,
    },
    types::{DoubleZeroEpoch, SolanaValidatorPayment},
    ID,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use svm_hash::merkle::{merkle_root_from_indexed_pod_leaves, MerkleProof};

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

    // Odd-leaf merkle tree.
    let mut payments_data = (0..511)
        .map(|i| SolanaValidatorPayment {
            node_id: Pubkey::new_unique(),
            amount: 100_000_000_000 * (i + 1),
        })
        .collect::<Vec<_>>();
    assert_eq!(payments_data.len() % 2, 1);

    let merkle_root = merkle_root_from_indexed_pod_leaves(
        &payments_data,
        Some(SolanaValidatorPayment::LEAF_PREFIX),
    )
    .unwrap();

    let total_debt = payments_data.iter().map(|payment| payment.amount).sum();

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
                    total_validators: payments_data.len() as u32,
                    total_debt,
                    merkle_root,
                },
            ],
        )
        .await
        .unwrap();

    // Chunk into 64 instructions.
    let mut chunk = Vec::with_capacity(64);
    let last_index = payments_data.len() - 1;

    for (i, payment) in payments_data.iter().copied().enumerate() {
        let kind = DistributionMerkleRootKind::SolanaValidatorPayment(payment);
        let proof = MerkleProof::from_indexed_pod_leaves(
            &payments_data,
            i.try_into().unwrap(),
            Some(SolanaValidatorPayment::LEAF_PREFIX),
        )
        .unwrap();

        chunk.push((kind, proof));

        if chunk.len() == 64 || i == last_index {
            test_setup
                .verify_distribution_merkle_root(dz_epoch, chunk.clone())
                .await
                .unwrap();
            chunk.clear();
        }
    }

    // Attempt to spoof a replay attack with the last leaf of the odd-leaf
    // Merkle tree by duplicating the last leaf.
    let last_leaf = *payments_data.last().unwrap();
    payments_data.push(last_leaf);

    let invalid_merkle_root = merkle_root_from_indexed_pod_leaves(
        &payments_data,
        Some(SolanaValidatorPayment::LEAF_PREFIX),
    )
    .unwrap();
    assert_ne!(merkle_root, invalid_merkle_root);

    let spoofed_proof = MerkleProof::from_indexed_pod_leaves(
        &payments_data,
        payments_data.len() as u32 - 1,
        Some(SolanaValidatorPayment::LEAF_PREFIX),
    )
    .unwrap();

    let verify_distribution_merkle_root_ix = try_build_instruction(
        &ID,
        VerifyDistributionMerkleRootAccounts::new(dz_epoch),
        &RevenueDistributionInstructionData::VerifyDistributionMerkleRoot {
            kind: DistributionMerkleRootKind::SolanaValidatorPayment(last_leaf),
            proof: spoofed_proof,
        },
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(&[verify_distribution_merkle_root_ix], &[])
        .await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidInstructionData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        &format!("Program log: Invalid computed merkle root: {invalid_merkle_root}")
    );
}
