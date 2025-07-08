use crate::constants::PROPORTION_SCALE_FACTOR;
use anyhow::{Result, anyhow};
use borsh::{BorshDeserialize, BorshSerialize};
use rust_decimal::{Decimal, prelude::ToPrimitive};
use solana_sdk::pubkey::Pubkey;
use svm_hash::{merkle::*, sha2::Hash};

/// Represents a single contributor's calculated reward proportion.
#[derive(Debug, Clone, BorshSerialize, BorshDeserialize)]
pub struct ContributorReward {
    pub payee: Pubkey,
    // Scaled by PROPORTION_SCALE_FACTOR
    pub proportion: u64,
}

/// Represents the burn information for the epoch.
#[derive(Debug, Clone, BorshSerialize, BorshDeserialize)]
pub struct BurnInfo {
    pub rate: u64,
}

/// Leaf of the merkle tree
#[derive(Debug, Clone, BorshSerialize, BorshDeserialize)]
pub enum MerkleLeafData {
    ContributorReward(ContributorReward),
    Burn(BurnInfo),
}

/// The final, canonical output of a successful calculation run
#[derive(Debug, Clone)]
pub struct CalculationOutput {
    /// The Merkle root hash to be published on-chain.
    pub merkle_root: Hash,
    /// The full list of leaves, to be published to the DZ Ledger.
    /// This list MUST be sorted to ensure the tree is reproducible.
    pub merkle_leaves: Vec<MerkleLeafData>,
}

/// Holds the complete merkle tree data for proof generation
#[derive(Debug, Clone)]
pub struct MerkleTreeData {
    /// The merkle root hash
    pub root: Hash,
    // TODO: We should really just store this pre-sorted or use a different structure?
    /// The original, unsorted leaf data
    pub original_leaves: Vec<MerkleLeafData>,
    /// The sorted, serialized leaves used to build the tree
    pub sorted_serialized_leaves: Vec<Vec<u8>>,
}

impl MerkleTreeData {
    /// Generate a merkle proof for a specific payee
    pub fn get_proof_for_payee(&self, payee: &Pubkey) -> Result<MerkleProof> {
        // Find the original leaf data for this payee
        let user_leaf_data = self
            .original_leaves
            .iter()
            .find(|leaf| {
                if let MerkleLeafData::ContributorReward(reward) = leaf {
                    &reward.payee == payee
                } else {
                    false
                }
            })
            .ok_or_else(|| anyhow!("Payee not found in merkle tree"))?;

        // Serialize it to find its byte representation
        let user_leaf_bytes = borsh::to_vec(user_leaf_data)
            .map_err(|e| anyhow!("Failed to serialize user leaf: {}", e))?;

        // Find the index of this leaf in the sorted list
        let leaf_index = self
            .sorted_serialized_leaves
            .binary_search(&user_leaf_bytes)
            .map_err(|_| anyhow!("Serialized leaf not found in sorted list"))?;

        // Convert sorted leaves to the format expected by svm-hash
        let leaf_slices: Vec<&[u8]> = self
            .sorted_serialized_leaves
            .iter()
            .map(|v| v.as_slice())
            .collect();

        // Generate the proof
        MerkleProof::from_leaves(&leaf_slices, leaf_index)
            .ok_or_else(|| anyhow!("Failed to generate merkle proof"))
    }

    /// Verify a proof against the expected root
    pub fn verify_proof(&self, proof: &MerkleProof, leaf_bytes: &[u8]) -> bool {
        proof.root_from_leaf(leaf_bytes) == self.root
    }
}

/// Generate a Merkle tree from rewards and burn rate
// TODO: This should just be impl MerkleLeafData { fn from_rewards_with_burn_rate() -> Result<Self> }
pub fn generate_tree(
    rewards: &[(String, Decimal)], // (operator_pubkey, proportion)
    burn_rate: u64,
) -> Result<MerkleTreeData> {
    // Convert rewards to MerkleLeafData
    let mut leaves = Vec::new();

    for (operator, proportion) in rewards {
        let scaled_proportion =
            (proportion.to_f64().unwrap_or(0.0) * PROPORTION_SCALE_FACTOR as f64) as u64;

        // FIXME: Parse operator string as Pubkey
        let payee = operator
            .parse::<Pubkey>()
            .unwrap_or_else(|_| Pubkey::default());

        leaves.push(MerkleLeafData::ContributorReward(ContributorReward {
            payee,
            proportion: scaled_proportion,
        }));
    }

    // Add burn info
    leaves.push(MerkleLeafData::Burn(BurnInfo { rate: burn_rate }));

    // Keep a copy of the original leaves
    let original_leaves = leaves.clone();

    // Serialize all leaves to bytes using Borsh
    let mut serialized_leaves: Vec<Vec<u8>> = leaves
        .iter()
        .map(borsh::to_vec)
        .collect::<Result<Vec<_>, _>>()
        .map_err(|e| anyhow!("Failed to serialize leaf: {}", e))?;

    // Sort serialized leaves lexicographically for deterministic ordering
    serialized_leaves.sort();

    // Convert to the format expected by svm-hash
    let leaf_slices: Vec<&[u8]> = serialized_leaves.iter().map(|v| v.as_slice()).collect();

    // Generate the merkle root
    let root = merkle_root_from_leaves(&leaf_slices)
        .ok_or_else(|| anyhow!("Failed to generate merkle root: empty tree"))?;

    Ok(MerkleTreeData {
        root,
        original_leaves,
        sorted_serialized_leaves: serialized_leaves,
    })
}

/// Calculate burn rate for the epoch
/// FIXME: Simple formula for now: min(max_burn_rate, epoch_number * coefficient)
// TODO: Does this need to be public?
pub fn calculate_burn_rate(epoch: u64, coefficient: u64, max_burn_rate: u64) -> u64 {
    let raw_burn_rate = epoch.saturating_mul(coefficient);
    raw_burn_rate.min(max_burn_rate)
}

#[cfg(test)]
mod tests {
    use super::*;
    use rust_decimal::dec;

    #[test]
    fn test_borsh_serialization_determinism() {
        // Test that the same data always serializes to the same bytes
        let reward = ContributorReward {
            payee: Pubkey::new_unique(),
            proportion: 500_000_000_000_000, // 0.5 scaled
        };

        let bytes1 = borsh::to_vec(&reward).unwrap();
        let bytes2 = borsh::to_vec(&reward).unwrap();

        assert_eq!(bytes1, bytes2, "Serialization should be deterministic");
    }

    #[test]
    fn test_merkle_leaf_serialization() {
        // Test serialization of different leaf types
        let reward_leaf = MerkleLeafData::ContributorReward(ContributorReward {
            payee: Pubkey::new_unique(),
            proportion: 250_000_000_000_000, // 0.25 scaled
        });

        let burn_leaf = MerkleLeafData::Burn(BurnInfo { rate: 100 });

        let reward_bytes = borsh::to_vec(&reward_leaf).unwrap();
        let burn_bytes = borsh::to_vec(&burn_leaf).unwrap();

        // Verify enum tags are different
        assert_ne!(
            reward_bytes[0], burn_bytes[0],
            "Enum variants should have different tags"
        );

        // Verify we can deserialize back
        let deserialized_reward: MerkleLeafData = borsh::from_slice(&reward_bytes).unwrap();
        let deserialized_burn: MerkleLeafData = borsh::from_slice(&burn_bytes).unwrap();

        match deserialized_reward {
            MerkleLeafData::ContributorReward(_) => (),
            _ => panic!("Expected ContributorReward variant"),
        }

        match deserialized_burn {
            MerkleLeafData::Burn(_) => (),
            _ => panic!("Expected Burn variant"),
        }
    }

    #[test]
    fn test_lexicographic_sorting() {
        // Create multiple leaves in an order that will change when sorted
        // Burn leaf (tag 1) should come after Reward leaves (tag 0) when sorted
        let leaves = [
            MerkleLeafData::Burn(BurnInfo { rate: 50 }),
            MerkleLeafData::ContributorReward(ContributorReward {
                payee: Pubkey::new_from_array([255; 32]), // High value pubkey
                proportion: 100,
            }),
            MerkleLeafData::ContributorReward(ContributorReward {
                payee: Pubkey::new_from_array([1; 32]), // Low value pubkey
                proportion: 200,
            }),
        ];

        // Serialize all leaves
        let mut serialized: Vec<Vec<u8>> = leaves
            .iter()
            .map(|leaf| borsh::to_vec(leaf).unwrap())
            .collect();

        // Sort them
        let original = serialized.clone();
        serialized.sort();

        // Verify that sorting changed the order
        assert_ne!(original, serialized, "Sorting should change the order");

        // Verify the order is consistent
        let mut resorted = serialized.clone();
        resorted.sort();
        assert_eq!(serialized, resorted, "Sorting should be stable");

        // Verify expected order: Reward leaves (tag 0) come before Burn leaf (tag 1)
        assert_eq!(serialized[0][0], 0, "First leaf should be a reward (tag 0)");
        assert_eq!(
            serialized[1][0], 0,
            "Second leaf should be a reward (tag 0)"
        );
        assert_eq!(serialized[2][0], 1, "Third leaf should be burn (tag 1)");
    }

    #[test]
    fn test_generate_tree_basic() {
        let rewards = vec![
            ("11111111111111111111111111111111".to_string(), dec!(0.5)),
            ("22222222222222222222222222222222".to_string(), dec!(0.3)),
            ("33333333333333333333333333333333".to_string(), dec!(0.2)),
        ];

        let burn_rate = 100;
        let tree_data = generate_tree(&rewards, burn_rate).unwrap();

        // Verify we have the right number of leaves
        assert_eq!(tree_data.original_leaves.len(), 4); // 3 rewards + 1 burn
        assert_eq!(tree_data.sorted_serialized_leaves.len(), 4);

        // Verify root is not zero
        assert_ne!(tree_data.root, Hash::default());
    }

    #[test]
    fn test_generate_tree_deterministic() {
        let rewards = vec![
            ("44444444444444444444444444444444".to_string(), dec!(0.6)),
            ("55555555555555555555555555555555".to_string(), dec!(0.4)),
        ];

        let burn_rate = 200;

        // Generate tree twice with same inputs
        let tree1 = generate_tree(&rewards, burn_rate).unwrap();
        let tree2 = generate_tree(&rewards, burn_rate).unwrap();

        // Roots should be identical
        assert_eq!(
            tree1.root, tree2.root,
            "Merkle root should be deterministic"
        );
    }

    #[test]
    fn test_get_proof_for_payee() {
        let payee1 = Pubkey::new_unique();
        let payee2 = Pubkey::new_unique();

        let rewards = vec![
            (payee1.to_string(), dec!(0.7)),
            (payee2.to_string(), dec!(0.3)),
        ];

        let tree_data = generate_tree(&rewards, 50).unwrap();

        // Get proof for first payee
        let proof = tree_data.get_proof_for_payee(&payee1).unwrap();

        // Find the leaf data for verification
        let leaf_data = tree_data
            .original_leaves
            .iter()
            .find(|leaf| match leaf {
                MerkleLeafData::ContributorReward(r) => r.payee == payee1,
                _ => false,
            })
            .unwrap();

        let leaf_bytes = borsh::to_vec(leaf_data).unwrap();

        // Verify the proof
        assert!(tree_data.verify_proof(&proof, &leaf_bytes));
    }

    #[test]
    fn test_get_proof_for_nonexistent_payee() {
        let rewards = vec![("66666666666666666666666666666666".to_string(), dec!(1.0))];

        let tree_data = generate_tree(&rewards, 100).unwrap();
        let nonexistent = Pubkey::new_unique();

        // Should fail to get proof for non-existent payee
        assert!(tree_data.get_proof_for_payee(&nonexistent).is_err());
    }

    #[test]
    fn test_burn_rate_calculation() {
        assert_eq!(calculate_burn_rate(10, 5, 100), 50);
        assert_eq!(calculate_burn_rate(50, 5, 100), 100); // Capped at max
        assert_eq!(calculate_burn_rate(0, 5, 100), 0);

        // Test overflow protection
        assert_eq!(calculate_burn_rate(u64::MAX, 2, 1000), 1000);
    }

    #[test]
    fn test_single_leaf_tree() {
        let rewards = vec![];
        let burn_rate = 75;

        let tree_data = generate_tree(&rewards, burn_rate).unwrap();

        // Should only have burn leaf
        assert_eq!(tree_data.original_leaves.len(), 1);
        match &tree_data.original_leaves[0] {
            MerkleLeafData::Burn(burn) => assert_eq!(burn.rate, 75),
            _ => panic!("Expected burn leaf"),
        }
    }
}
