mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::VerifyDistributionMerkleRootAccounts, DistributionMerkleRootKind,
        ProgramConfiguration, ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    types::{DoubleZeroEpoch, RewardShare, SolanaValidatorDebt},
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

    let debt_accountant_signer = Keypair::new();
    let rewards_accountant_signer = Keypair::new();
    let solana_validator_base_block_rewards_pct_fee = 500; // 5%.

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Relay settings.
    let distribute_rewards_relay_lamports = 10_000;

    // Distribution.

    let dz_epoch = DoubleZeroEpoch::new(1);

    // Odd-leaf merkle tree.
    let mut debt_data = (0..511)
        .map(|i| SolanaValidatorDebt {
            node_id: Pubkey::new_unique(),
            amount: 100_000_000_000 * (i + 1),
        })
        .collect::<Vec<_>>();
    assert_eq!(debt_data.len() % 2, 1);

    let solana_validator_debt_merkle_root =
        merkle_root_from_indexed_pod_leaves(&debt_data, Some(SolanaValidatorDebt::LEAF_PREFIX))
            .unwrap();

    let total_debt = debt_data.iter().map(|debt| debt.amount).sum();

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
                ProgramConfiguration::DebtAccountant(debt_accountant_signer.pubkey()),
                ProgramConfiguration::RewardsAccountant(rewards_accountant_signer.pubkey()),
                ProgramConfiguration::SolanaValidatorFeeParameters {
                    base_block_rewards_pct: solana_validator_base_block_rewards_pct_fee,
                    priority_block_rewards_pct: 0,
                    inflation_rewards_pct: 0,
                    jito_tips_pct: 0,
                    fixed_sol_amount: 0,
                    _unused: Default::default(),
                },
                ProgramConfiguration::CommunityBurnRateParameters {
                    limit: cbr_limit,
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
                },
                ProgramConfiguration::DistributeRewardsRelayLamports(
                    distribute_rewards_relay_lamports,
                ),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .configure_distribution_debt(
            dz_epoch,
            &debt_accountant_signer,
            debt_data.len() as u32,
            total_debt,
            solana_validator_debt_merkle_root,
        )
        .await
        .unwrap();

    // Chunk into 64 instructions.
    let mut chunk = Vec::with_capacity(64);
    let last_index = debt_data.len() - 1;

    for (i, debt) in debt_data.iter().copied().enumerate() {
        let kind = DistributionMerkleRootKind::SolanaValidatorDebt(debt);
        let proof = MerkleProof::from_indexed_pod_leaves(
            &debt_data,
            i.try_into().unwrap(),
            Some(SolanaValidatorDebt::LEAF_PREFIX),
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
    let last_leaf = *debt_data.last().unwrap();
    debt_data.push(last_leaf);

    let invalid_merkle_root =
        merkle_root_from_indexed_pod_leaves(&debt_data, Some(SolanaValidatorDebt::LEAF_PREFIX))
            .unwrap();
    assert_ne!(solana_validator_debt_merkle_root, invalid_merkle_root);

    let spoofed_proof = MerkleProof::from_indexed_pod_leaves(
        &debt_data,
        debt_data.len() as u32 - 1,
        Some(SolanaValidatorDebt::LEAF_PREFIX),
    )
    .unwrap();

    let verify_distribution_merkle_root_ix = try_build_instruction(
        &ID,
        VerifyDistributionMerkleRootAccounts::new(dz_epoch),
        &RevenueDistributionInstructionData::VerifyDistributionMerkleRoot {
            kind: DistributionMerkleRootKind::SolanaValidatorDebt(last_leaf),
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
        program_logs.get(3).unwrap(),
        "Program log: Solana validator debt 511"
    );
    assert_eq!(
        program_logs.get(4).unwrap(),
        &format!("Program log: Invalid computed merkle root: {invalid_merkle_root}")
    );

    // Distribution rewards.

    // Arbitrarily set one of the rewards to be blocked.
    let rewards_data = [
        RewardShare::new(Pubkey::new_unique(), 100_000_000, false, 0).unwrap(),
        RewardShare::new(Pubkey::new_unique(), 200_000_000, false, 0).unwrap(),
        RewardShare::new(Pubkey::new_unique(), 300_000_000, true, 0).unwrap(),
        RewardShare::new(Pubkey::new_unique(), 150_000_000, false, 0).unwrap(),
        RewardShare::new(Pubkey::new_unique(), 250_000_000, false, 0).unwrap(),
    ];
    assert_eq!(
        rewards_data
            .iter()
            .map(|rewards| rewards.unit_share)
            .sum::<u32>(),
        1_000_000_000
    );

    let total_contributors = rewards_data.len() as u32;
    let rewards_merkle_root =
        merkle_root_from_indexed_pod_leaves(&rewards_data, Some(RewardShare::LEAF_PREFIX)).unwrap();

    // Finalize distribution debt so we can post the rewards merkle root.
    test_setup
        .finalize_distribution_debt(dz_epoch, &debt_accountant_signer)
        .await
        .unwrap()
        .configure_distribution_rewards(
            dz_epoch,
            &rewards_accountant_signer,
            total_contributors,
            rewards_merkle_root,
        )
        .await
        .unwrap();

    let kinds_and_proofs = rewards_data
        .iter()
        .copied()
        .enumerate()
        .map(|(i, reward_share)| {
            let kind = DistributionMerkleRootKind::RewardShare(reward_share);
            let proof = MerkleProof::from_indexed_pod_leaves(
                &rewards_data,
                i.try_into().unwrap(),
                Some(RewardShare::LEAF_PREFIX),
            )
            .unwrap();

            (kind, proof)
        })
        .collect::<Vec<_>>();

    test_setup
        .verify_distribution_merkle_root(dz_epoch, kinds_and_proofs)
        .await
        .unwrap();
}
