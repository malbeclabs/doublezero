use rewards_calculator::merkle_generator::{MerkleLeafData, generate_tree};
use rust_decimal::{Decimal, dec};
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;

/// Test end-to-end proof verification for every leaf in the tree
#[test]
fn test_e2e_verify_all_leaves() {
    // Create a diverse set of rewards
    let rewards = vec![
        ("11111111111111111111111111111111".to_string(), dec!(0.3)),
        ("22222222222222222222222222222222".to_string(), dec!(0.25)),
        ("33333333333333333333333333333333".to_string(), dec!(0.2)),
        ("44444444444444444444444444444444".to_string(), dec!(0.15)),
        ("55555555555555555555555555555555".to_string(), dec!(0.1)),
    ];

    let burn_rate = 100;
    let tree_data = generate_tree(&rewards, burn_rate).unwrap();

    // Verify we can generate and verify a proof for EVERY leaf
    for (index, leaf) in tree_data.original_leaves.iter().enumerate() {
        let leaf_bytes = borsh::to_vec(leaf).unwrap();

        // Convert sorted leaves to the format expected by svm-hash
        let leaf_slices: Vec<&[u8]> = tree_data
            .sorted_serialized_leaves
            .iter()
            .map(|v| v.as_slice())
            .collect();

        // Find the index in the sorted list
        let sorted_index = tree_data
            .sorted_serialized_leaves
            .binary_search(&leaf_bytes)
            .expect(&format!("Leaf {index} should be in sorted list"));

        // Generate proof
        let proof = svm_hash::merkle::MerkleProof::from_leaves(&leaf_slices, sorted_index)
            .expect(&format!("Should generate proof for leaf {index}"));

        // Verify the proof produces the expected root
        let computed_root = proof.root_from_leaf(&leaf_bytes);
        assert_eq!(
            computed_root, tree_data.root,
            "Proof verification failed for leaf {index} (index {sorted_index})",
        );
    }
}

/// Test that proofs work correctly for edge cases
#[test]
fn test_e2e_edge_case_trees() {
    // Test 1: Single leaf (only burn)
    let empty_rewards = vec![];
    let tree_single = generate_tree(&empty_rewards, 50).unwrap();

    let burn_leaf = &tree_single.original_leaves[0];
    let burn_bytes = borsh::to_vec(burn_leaf).unwrap();

    // For a single leaf tree, the proof should be empty
    let leaf_slices: Vec<&[u8]> = vec![&burn_bytes];
    let proof = svm_hash::merkle::MerkleProof::from_leaves(&leaf_slices, 0).unwrap();

    assert_eq!(proof.root_from_leaf(&burn_bytes), tree_single.root);

    // Test 2: Two leaves (one reward + burn)
    let two_leaf_rewards = vec![("11111111111111111111111111111111".to_string(), dec!(1.0))];
    let tree_two = generate_tree(&two_leaf_rewards, 75).unwrap();

    // Verify both leaves
    for leaf in &tree_two.original_leaves {
        let leaf_bytes = borsh::to_vec(leaf).unwrap();

        match leaf {
            MerkleLeafData::ContributorReward(reward) => {
                let proof = tree_two.get_proof_for_payee(&reward.payee).unwrap();
                assert!(tree_two.verify_proof(&proof, &leaf_bytes));
            }
            MerkleLeafData::Burn(_) => {
                // For burn, we need to generate proof manually
                let sorted_index = tree_two
                    .sorted_serialized_leaves
                    .binary_search(&leaf_bytes)
                    .unwrap();

                let leaf_slices: Vec<&[u8]> = tree_two
                    .sorted_serialized_leaves
                    .iter()
                    .map(|v| v.as_slice())
                    .collect();

                let proof =
                    svm_hash::merkle::MerkleProof::from_leaves(&leaf_slices, sorted_index).unwrap();

                assert_eq!(proof.root_from_leaf(&leaf_bytes), tree_two.root);
            }
        }
    }

    // Test 3: Power of 2 leaves (4 rewards + burn = 5 total, but tree handles it)
    let power_rewards = vec![
        (
            "PWR1111111111111111111111111111111111111111".to_string(),
            dec!(0.25),
        ),
        (
            "PWR2222222222222222222222222222222222222222".to_string(),
            dec!(0.25),
        ),
        (
            "PWR3333333333333333333333333333333333333333".to_string(),
            dec!(0.25),
        ),
        (
            "PWR4444444444444444444444444444444444444444".to_string(),
            dec!(0.25),
        ),
    ];
    let tree_power = generate_tree(&power_rewards, 200).unwrap();

    // Verify all proofs
    for reward in power_rewards {
        let pubkey = Pubkey::from_str(&reward.0).unwrap();
        let proof = tree_power.get_proof_for_payee(&pubkey).unwrap();

        let leaf_data = tree_power
            .original_leaves
            .iter()
            .find(|leaf| match leaf {
                MerkleLeafData::ContributorReward(r) => r.payee == pubkey,
                _ => false,
            })
            .unwrap();

        let leaf_bytes = borsh::to_vec(leaf_data).unwrap();
        assert!(tree_power.verify_proof(&proof, &leaf_bytes));
    }
}

/// Test cross-validation failures
#[test]
fn test_e2e_negative_cases() {
    let rewards = vec![
        (
            "NEG1111111111111111111111111111111111111111".to_string(),
            dec!(0.6),
        ),
        (
            "NEG2222222222222222222222222222222222222222".to_string(),
            dec!(0.4),
        ),
    ];

    let tree = generate_tree(&rewards, 123).unwrap();

    let pubkey1 = Pubkey::from_str(&rewards[0].0).unwrap();
    let pubkey2 = Pubkey::from_str(&rewards[1].0).unwrap();

    let proof1 = tree.get_proof_for_payee(&pubkey1).unwrap();
    let proof2 = tree.get_proof_for_payee(&pubkey2).unwrap();

    // Get leaf data
    let leaf1 = tree
        .original_leaves
        .iter()
        .find(|l| match l {
            MerkleLeafData::ContributorReward(r) => r.payee == pubkey1,
            _ => false,
        })
        .unwrap();

    let leaf2 = tree
        .original_leaves
        .iter()
        .find(|l| match l {
            MerkleLeafData::ContributorReward(r) => r.payee == pubkey2,
            _ => false,
        })
        .unwrap();

    let bytes1 = borsh::to_vec(leaf1).unwrap();
    let bytes2 = borsh::to_vec(leaf2).unwrap();

    // Correct proofs should verify
    assert!(tree.verify_proof(&proof1, &bytes1));
    assert!(tree.verify_proof(&proof2, &bytes2));

    // Cross-verification should fail
    assert!(!tree.verify_proof(&proof1, &bytes2));
    assert!(!tree.verify_proof(&proof2, &bytes1));

    // Tampered data should fail
    let mut tampered_bytes = bytes1.clone();
    tampered_bytes[10] ^= 0xFF; // Flip some bits
    assert!(!tree.verify_proof(&proof1, &tampered_bytes));

    // Wrong proof with correct data should fail
    let fake_pubkey = Pubkey::new_unique();
    assert!(tree.get_proof_for_payee(&fake_pubkey).is_err());
}

/// Test with realistic epoch data
#[test]
fn test_e2e_realistic_epoch() {
    // Simulate a realistic epoch with many contributors
    let num_contributors = 50;
    let mut rewards = Vec::new();

    // Create a distribution that mimics real network behavior
    // Some large contributors, many medium, and some small
    for i in 0..num_contributors {
        // Create a deterministic pubkey using Pubkey::new_unique() converted to string
        // This ensures valid base58 pubkeys
        let deterministic_bytes = [i as u8; 32];
        let pubkey = Pubkey::new_from_array(deterministic_bytes).to_string();
        let proportion = if i < 5 {
            // Top 5 contributors get larger shares
            dec!(0.1) - (dec!(0.01) * Decimal::from(i))
        } else if i < 20 {
            // Next 15 get medium shares
            dec!(0.03)
        } else {
            // Remaining get small shares
            dec!(0.01)
        };
        rewards.push((pubkey, proportion));
    }

    // Normalize proportions to sum to 1.0
    let total: rust_decimal::Decimal = rewards.iter().map(|(_, p)| p).sum();
    let normalized_rewards: Vec<_> = rewards.into_iter().map(|(pk, p)| (pk, p / total)).collect();

    let burn_rate = 250; // 2.5% burn rate
    let tree = generate_tree(&normalized_rewards, burn_rate).unwrap();

    // Verify we have the expected number of leaves
    assert_eq!(tree.original_leaves.len(), num_contributors + 1);

    // Sample verification: Check a few random contributors
    let sample_indices = [0, 10, 25, 49];
    for &idx in &sample_indices {
        let pubkey = Pubkey::from_str(&normalized_rewards[idx].0).unwrap();
        let proof = tree.get_proof_for_payee(&pubkey).unwrap();

        let leaf = tree
            .original_leaves
            .iter()
            .find(|l| match l {
                MerkleLeafData::ContributorReward(r) => r.payee == pubkey,
                _ => false,
            })
            .unwrap();

        let leaf_bytes = borsh::to_vec(leaf).unwrap();
        assert!(
            tree.verify_proof(&proof, &leaf_bytes),
            "Failed to verify contributor at index {idx}",
        );
    }
}

/// Test that the same tree structure can be recreated from leaves
#[test]
fn test_e2e_tree_reconstruction() {
    let rewards = vec![
        (
            "REC1111111111111111111111111111111111111111".to_string(),
            dec!(0.5),
        ),
        (
            "REC2222222222222222222222222222222222222222".to_string(),
            dec!(0.3),
        ),
        (
            "REC3333333333333333333333333333333333333333".to_string(),
            dec!(0.2),
        ),
    ];

    let burn_rate = 175;
    let original_tree = generate_tree(&rewards, burn_rate).unwrap();

    // Simulate what would happen if someone downloads the leaves from DZ Ledger
    // and wants to verify the merkle root matches what's on-chain

    // They would have the sorted serialized leaves
    let downloaded_leaves = original_tree.sorted_serialized_leaves.clone();

    // Convert to the format expected by svm-hash
    let leaf_slices: Vec<&[u8]> = downloaded_leaves.iter().map(|v| v.as_slice()).collect();

    // Recreate the merkle root
    let reconstructed_root = svm_hash::merkle::merkle_root_from_leaves(&leaf_slices)
        .expect("Should reconstruct root from leaves");

    // The reconstructed root should match the original
    assert_eq!(
        reconstructed_root, original_tree.root,
        "Reconstructed merkle root doesn't match original"
    );

    // Also verify that each contributor can still generate valid proofs
    for (pubkey_str, _) in &rewards {
        let pubkey = Pubkey::from_str(pubkey_str).unwrap();
        let proof = original_tree.get_proof_for_payee(&pubkey).unwrap();

        let leaf = original_tree
            .original_leaves
            .iter()
            .find(|l| match l {
                MerkleLeafData::ContributorReward(r) => r.payee == pubkey,
                _ => false,
            })
            .unwrap();

        let leaf_bytes = borsh::to_vec(leaf).unwrap();

        // The proof should still verify against the reconstructed root
        assert_eq!(
            proof.root_from_leaf(&leaf_bytes),
            reconstructed_root,
            "Proof doesn't verify against reconstructed root"
        );
    }
}
