use rewards_calculator::{
    constants::PROPORTION_SCALE_FACTOR,
    merkle_generator::{MerkleLeafData, generate_tree},
};
use rust_decimal::dec;
use solana_sdk::pubkey::Pubkey;
use svm_hash::sha2::Hash;

/// Creates a deterministic set of test rewards
fn create_test_rewards() -> Vec<(String, rust_decimal::Decimal)> {
    vec![
        // Use deterministic pubkeys for reproducible tests
        (
            "ALiCE111111111111111111111111111111111111111".to_string(),
            dec!(0.4),
        ),
        (
            "BoB1111111111111111111111111111111111111111".to_string(),
            dec!(0.35),
        ),
        (
            "CARoL11111111111111111111111111111111111111".to_string(),
            dec!(0.25),
        ),
    ]
}

#[test]
fn test_merkle_tree_snapshot() {
    let rewards = create_test_rewards();
    let burn_rate = 150;

    let tree_data = generate_tree(&rewards, burn_rate).unwrap();

    // This is our "golden" hash - computed once and verified manually
    // If the merkle generation logic changes, this test will fail
    // Update this hash only after verifying the change is intentional
    let expected_root = "9ajPb91SN5dGEaJdzQZGAw5i7REZaLTFAmS8JZLLxg5S";

    assert_eq!(
        tree_data.root.to_string(),
        expected_root,
        "Merkle root changed! Verify this is intentional before updating the snapshot."
    );

    // Verify leaf count
    assert_eq!(tree_data.original_leaves.len(), 4); // 3 rewards + 1 burn

    // Verify leaf types and values
    let mut reward_count = 0;
    let mut burn_count = 0;

    for leaf in &tree_data.original_leaves {
        match leaf {
            MerkleLeafData::ContributorReward(reward) => {
                reward_count += 1;
                // Verify proportion is scaled correctly
                assert!(reward.proportion > 0);
                assert!(reward.proportion <= PROPORTION_SCALE_FACTOR);
            }
            MerkleLeafData::Burn(burn) => {
                burn_count += 1;
                assert_eq!(burn.rate, 150);
            }
        }
    }

    assert_eq!(reward_count, 3);
    assert_eq!(burn_count, 1);
}

#[test]
fn test_merkle_tree_determinism_integration() {
    let rewards = create_test_rewards();
    let burn_rate = 200;

    // Generate tree multiple times
    let roots: Vec<Hash> = (0..5)
        .map(|_| generate_tree(&rewards, burn_rate).unwrap().root)
        .collect();

    // All roots should be identical
    for root in &roots[1..] {
        assert_eq!(
            roots[0], *root,
            "Merkle root generation is not deterministic!"
        );
    }
}

#[test]
fn test_merkle_tree_different_inputs() {
    let rewards1 = create_test_rewards();
    let rewards2 = vec![
        (
            "DAVE111111111111111111111111111111111111111".to_string(),
            dec!(0.5),
        ),
        (
            "EVE1111111111111111111111111111111111111111".to_string(),
            dec!(0.5),
        ),
    ];

    let tree1 = generate_tree(&rewards1, 100).unwrap();
    let tree2 = generate_tree(&rewards2, 100).unwrap();

    // Different inputs should produce different roots
    assert_ne!(tree1.root, tree2.root);

    // Same rewards but different burn rate should produce different roots
    let tree3 = generate_tree(&rewards1, 200).unwrap();
    assert_ne!(tree1.root, tree3.root);
}

#[test]
fn test_merkle_proof_integration() {
    let alice = "ALiCE111111111111111111111111111111111111111";
    let bob = "BoB1111111111111111111111111111111111111111";

    let rewards = vec![(alice.to_string(), dec!(0.6)), (bob.to_string(), dec!(0.4))];

    let tree_data = generate_tree(&rewards, 75).unwrap();

    // Get proofs for both payees
    let alice_pubkey = alice.parse::<Pubkey>().unwrap();
    let bob_pubkey = bob.parse::<Pubkey>().unwrap();

    let alice_proof = tree_data.get_proof_for_payee(&alice_pubkey).unwrap();
    let bob_proof = tree_data.get_proof_for_payee(&bob_pubkey).unwrap();

    // Find the leaf data
    let alice_leaf = tree_data
        .original_leaves
        .iter()
        .find(|leaf| match leaf {
            MerkleLeafData::ContributorReward(r) => r.payee == alice_pubkey,
            _ => false,
        })
        .unwrap();

    let bob_leaf = tree_data
        .original_leaves
        .iter()
        .find(|leaf| match leaf {
            MerkleLeafData::ContributorReward(r) => r.payee == bob_pubkey,
            _ => false,
        })
        .unwrap();

    // Serialize leaves
    let alice_bytes = borsh::to_vec(alice_leaf).unwrap();
    let bob_bytes = borsh::to_vec(bob_leaf).unwrap();

    // Verify proofs
    assert!(tree_data.verify_proof(&alice_proof, &alice_bytes));
    assert!(tree_data.verify_proof(&bob_proof, &bob_bytes));

    // Cross-verification should fail
    assert!(!tree_data.verify_proof(&alice_proof, &bob_bytes));
    assert!(!tree_data.verify_proof(&bob_proof, &alice_bytes));
}

#[test]
fn test_edge_cases_integration() {
    // Empty rewards (only burn)
    let empty_rewards = vec![];
    let tree_empty = generate_tree(&empty_rewards, 50).unwrap();
    assert_eq!(tree_empty.original_leaves.len(), 1);

    // Single reward
    let single_reward = vec![(
        "SoLo111111111111111111111111111111111111111".to_string(),
        dec!(1.0),
    )];
    let tree_single = generate_tree(&single_reward, 100).unwrap();
    assert_eq!(tree_single.original_leaves.len(), 2); // 1 reward + 1 burn

    // Many rewards
    let many_rewards: Vec<(String, rust_decimal::Decimal)> = (0..100)
        .map(|i| {
            let pubkey = format!("USER{i:03}111111111111111111111111111111111111");
            let proportion = dec!(1.0) / dec!(100);
            (pubkey, proportion)
        })
        .collect();

    let tree_many = generate_tree(&many_rewards, 200).unwrap();
    assert_eq!(tree_many.original_leaves.len(), 101); // 100 rewards + 1 burn
}
