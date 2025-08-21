use anyhow::{Result, anyhow, bail};
use borsh::{BorshDeserialize, BorshSerialize};
use network_shapley::shapley::ShapleyOutput;
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;
use svm_hash::{
    merkle::{MerkleProof, merkle_root_from_byte_ref_leaves},
    sha2::Hash,
};

const LEAF_PREFIX: &[u8] = b"dz_contributor_rewards";

/// Represents a contributor's reward with Pubkey and u32 proportion (9-decimal precision)
/// where 1_000_000_000 = 100%
#[derive(Debug, Clone, BorshDeserialize, BorshSerialize)]
pub struct ContributorRewardDetail {
    // contributor.owner
    pub contributor_key: Pubkey,
    // 9-decimal proportion (1_000_000_000 = 100%)
    pub proportion: u32,
    // TODO: Add is_claimable: bool in future PR
}

impl ContributorRewardDetail {
    pub const LEAF_PREFIX: &'static [u8] = LEAF_PREFIX;
}

#[derive(Debug, Clone, BorshDeserialize, BorshSerialize)]
pub struct ContributorRewardsMerkleRoot {
    pub epoch: u64,
    pub root: Hash,
    pub total_contributors: u32,
}

/// Storage structure for consolidated shapley output
/// This is what gets stored on-chain instead of individual proofs
#[derive(Debug, Clone, BorshDeserialize, BorshSerialize)]
pub struct ShapleyOutputStorage {
    pub epoch: u64,
    pub rewards: Vec<ContributorRewardDetail>,
    pub total_proportions: u32, // Should equal 1_000_000_000 for validation
}

#[derive(Debug)]
pub struct ContributorRewardsMerkleTree {
    epoch: u64,
    rewards: Vec<ContributorRewardDetail>,
    leaves: Vec<Vec<u8>>,
}

impl ContributorRewardsMerkleTree {
    pub fn new(epoch: u64, shapley_output: &ShapleyOutput) -> Result<Self> {
        let mut rewards = Vec::new();
        let mut total_proportions: u32 = 0;

        for (operator_pubkey_str, val) in shapley_output.iter() {
            // Parse the operator string as a Pubkey
            let contributor_key = Pubkey::from_str(operator_pubkey_str)
                .map_err(|e| anyhow!("Invalid pubkey string '{}': {}", operator_pubkey_str, e))?;

            // Convert f64 proportion to u32 with 9 decimal places
            let proportion = (val.proportion * 1_000_000_000.0).round() as u32;
            total_proportions = total_proportions.saturating_add(proportion);

            rewards.push(ContributorRewardDetail {
                contributor_key,
                proportion,
            });
        }

        // Validate that proportions sum to approximately 1_000_000_000 (allowing small rounding errors)
        if !(999_999_000..=1_000_001_000).contains(&total_proportions) {
            bail!(
                "Total proportions {} not equal to 1_000_000_000 (±0.0001%)",
                total_proportions
            );
        }

        let leaves: Vec<Vec<u8>> = rewards
            .iter()
            .map(|reward| {
                borsh::to_vec(reward).map_err(|e| anyhow!("Failed to serialize reward: {}", e))
            })
            .collect::<Result<Vec<_>>>()?;

        Ok(Self {
            epoch,
            rewards,
            leaves,
        })
    }

    /// Compute the merkle root for all contributor rewards
    pub fn compute_root(&self) -> Result<Hash> {
        merkle_root_from_byte_ref_leaves(&self.leaves, Some(LEAF_PREFIX))
            .ok_or_else(|| anyhow!("Failed to compute merkle root for epoch {}", self.epoch))
    }

    /// Generate a proof for a specific contributor by index
    pub fn generate_proof(&self, contributor_index: usize) -> Result<MerkleProof> {
        if contributor_index >= self.leaves.len() {
            return Err(anyhow!(
                "Invalid contributor index {} for epoch {}. Total contributors: {}",
                contributor_index,
                self.epoch,
                self.leaves.len()
            ));
        }

        MerkleProof::from_byte_ref_leaves(&self.leaves, contributor_index as u32, Some(LEAF_PREFIX))
            .ok_or_else(|| {
                anyhow!(
                    "Failed to generate proof for contributor {} at epoch {}",
                    contributor_index,
                    self.epoch
                )
            })
    }

    /// Get reward detail by index (for verification)
    pub fn get_reward(&self, index: usize) -> Option<&ContributorRewardDetail> {
        self.rewards.get(index)
    }

    /// Get all rewards (for display)
    pub fn rewards(&self) -> &[ContributorRewardDetail] {
        &self.rewards
    }

    pub fn epoch(&self) -> u64 {
        self.epoch
    }

    /// Total number of contributors
    pub fn len(&self) -> usize {
        self.rewards.len()
    }

    /// Check if there are no contributors
    pub fn is_empty(&self) -> bool {
        self.rewards.is_empty()
    }
}

/// Generate a merkle proof dynamically from stored shapley output
pub fn generate_proof_from_shapley(
    shapley_storage: &ShapleyOutputStorage,
    contributor_pubkey: &Pubkey,
) -> Result<(MerkleProof, ContributorRewardDetail, Hash)> {
    // Find the contributor in the rewards list
    let mut contributor_index = None;
    let mut contributor_reward = None;

    for (index, reward) in shapley_storage.rewards.iter().enumerate() {
        if reward.contributor_key == *contributor_pubkey {
            contributor_index = Some(index);
            contributor_reward = Some(reward.clone());
            break;
        }
    }

    let index = contributor_index.ok_or_else(|| {
        anyhow!(
            "Contributor {} not found in shapley output",
            contributor_pubkey
        )
    })?;
    let reward = contributor_reward.unwrap();

    // Reconstruct the merkle tree from the stored rewards
    let leaves: Vec<Vec<u8>> = shapley_storage
        .rewards
        .iter()
        .map(|r| borsh::to_vec(r).map_err(|e| anyhow!("Failed to serialize reward: {}", e)))
        .collect::<Result<Vec<_>>>()?;

    // Generate the proof
    let proof = MerkleProof::from_byte_ref_leaves(&leaves, index as u32, Some(LEAF_PREFIX))
        .ok_or_else(|| {
            anyhow!(
                "Failed to generate proof for contributor at index {}",
                index
            )
        })?;

    // Compute the root for verification
    let root = merkle_root_from_byte_ref_leaves(&leaves, Some(LEAF_PREFIX))
        .ok_or_else(|| anyhow!("Failed to compute merkle root"))?;

    Ok((proof, reward, root))
}

// Deprecated functions have been removed - use generate_proof_from_shapley instead

#[cfg(test)]
mod tests {
    use super::*;
    use network_shapley::shapley::ShapleyValue;

    fn create_test_shapley_output() -> ShapleyOutput {
        let mut output = ShapleyOutput::new();
        output.insert(
            "11111111111111111111111111111112".to_string(), // Alice pubkey
            ShapleyValue {
                value: 100.0,
                proportion: 0.5,
            },
        );
        output.insert(
            "11111111111111111111111111111113".to_string(), // Bob pubkey
            ShapleyValue {
                value: 50.0,
                proportion: 0.25,
            },
        );
        output.insert(
            "11111111111111111111111111111114".to_string(), // Charlie pubkey
            ShapleyValue {
                value: 50.0,
                proportion: 0.25,
            },
        );
        output
    }

    fn create_single_contributor_output() -> ShapleyOutput {
        let mut output = ShapleyOutput::new();
        output.insert(
            "11111111111111111111111111111115".to_string(), // Solo pubkey
            ShapleyValue {
                value: 200.0,
                proportion: 1.0,
            },
        );
        output
    }

    fn create_empty_output() -> ShapleyOutput {
        ShapleyOutput::new()
    }

    #[test]
    fn test_merkle_tree_creation() {
        let output = create_test_shapley_output();
        let tree = ContributorRewardsMerkleTree::new(123, &output).unwrap();

        assert_eq!(tree.epoch(), 123);
        assert_eq!(tree.len(), 3);
        assert!(!tree.is_empty());

        // Check rewards are properly stored
        let rewards = tree.rewards();
        assert_eq!(rewards.len(), 3);

        // Find each contributor in rewards by their pubkey
        let alice_pubkey = Pubkey::from_str("11111111111111111111111111111112").unwrap();
        let alice = rewards
            .iter()
            .find(|r| r.contributor_key == alice_pubkey)
            .unwrap();
        assert_eq!(alice.proportion, 500_000_000); // 0.5 * 1_000_000_000

        let bob_pubkey = Pubkey::from_str("11111111111111111111111111111113").unwrap();
        let bob = rewards
            .iter()
            .find(|r| r.contributor_key == bob_pubkey)
            .unwrap();
        assert_eq!(bob.proportion, 250_000_000); // 0.25 * 1_000_000_000
    }

    #[test]
    fn test_single_contributor_tree() {
        let output = create_single_contributor_output();
        let tree = ContributorRewardsMerkleTree::new(456, &output).unwrap();

        assert_eq!(tree.len(), 1);
        assert!(!tree.is_empty());

        let root = tree.compute_root().unwrap();
        assert_ne!(root, Hash::default());

        // Generate proof for the single contributor
        let proof = tree.generate_proof(0).unwrap();

        // Verify the proof
        let reward = tree.get_reward(0).unwrap();
        let leaf = borsh::to_vec(reward).unwrap();
        let computed_root = proof.root_from_leaf(&leaf, Some(LEAF_PREFIX));

        assert_eq!(computed_root, root);
    }

    #[test]
    fn test_empty_tree() {
        let output = create_empty_output();
        // Empty tree will fail validation because proportions sum to 0, not 1_000_000_000
        let result = ContributorRewardsMerkleTree::new(789, &output);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("Total proportions")
        )
    }

    #[test]
    fn test_merkle_root_computation() {
        let output = create_test_shapley_output();
        let tree = ContributorRewardsMerkleTree::new(100, &output).unwrap();

        let root1 = tree.compute_root().unwrap();
        let root2 = tree.compute_root().unwrap();

        // Root should be deterministic
        assert_eq!(root1, root2);

        // Root should not be default/zero
        assert_ne!(root1, Hash::default());
    }

    #[test]
    fn test_proof_generation_and_verification() {
        let output = create_test_shapley_output();
        let tree = ContributorRewardsMerkleTree::new(200, &output).unwrap();
        let root = tree.compute_root().unwrap();

        // Test proof for each contributor
        for i in 0..tree.len() {
            let proof = tree.generate_proof(i).unwrap();
            let reward = tree.get_reward(i).unwrap();

            // Serialize reward to create leaf
            let leaf = borsh::to_vec(reward).unwrap();

            // Verify proof
            let computed_root = proof.root_from_leaf(&leaf, Some(LEAF_PREFIX));
            assert_eq!(
                computed_root, root,
                "Proof verification failed for contributor at index {i}",
            );
        }
    }

    #[test]
    fn test_invalid_proof_index() {
        let output = create_test_shapley_output();
        let tree = ContributorRewardsMerkleTree::new(300, &output).unwrap();

        // Try to generate proof for invalid index
        let result = tree.generate_proof(100);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("Invalid contributor index")
        );
    }

    #[test]
    fn test_proof_serialization_deserialization() {
        let output = create_test_shapley_output();
        let tree = ContributorRewardsMerkleTree::new(400, &output).unwrap();
        let root = tree.compute_root().unwrap();

        // Generate proof
        let proof = tree.generate_proof(0).unwrap();

        // Serialize proof
        let proof_bytes = borsh::to_vec(&proof).unwrap();

        // Deserialize proof
        let deserialized_proof: MerkleProof = borsh::from_slice(&proof_bytes).unwrap();

        // Verify deserialized proof works
        let reward = tree.get_reward(0).unwrap();
        let leaf = borsh::to_vec(reward).unwrap();
        let computed_root = deserialized_proof.root_from_leaf(&leaf, Some(LEAF_PREFIX));

        assert_eq!(computed_root, root);
    }

    #[test]
    fn test_generate_proof_from_shapley() {
        let output = create_test_shapley_output();
        let tree = ContributorRewardsMerkleTree::new(600, &output).unwrap();

        // Create ShapleyOutputStorage
        let shapley_storage = ShapleyOutputStorage {
            epoch: 600,
            rewards: tree.rewards().to_vec(),
            total_proportions: tree.rewards().iter().map(|r| r.proportion).sum(),
        };

        // Test generating proof for Alice
        let alice_pubkey = Pubkey::from_str("11111111111111111111111111111112").unwrap();
        let (proof, reward, root) =
            generate_proof_from_shapley(&shapley_storage, &alice_pubkey).unwrap();

        assert_eq!(reward.contributor_key, alice_pubkey);
        assert_eq!(reward.proportion, 500_000_000); // 0.5 * 1_000_000_000

        // Verify the proof
        let leaf = borsh::to_vec(&reward).unwrap();
        let computed_root = proof.root_from_leaf(&leaf, Some(LEAF_PREFIX));
        assert_eq!(computed_root, root);

        // Test for non-existent contributor
        let fake_pubkey = Pubkey::from_str("11111111111111111111111111111199").unwrap();
        let result = generate_proof_from_shapley(&shapley_storage, &fake_pubkey);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("not found"));
    }

    #[test]
    fn test_different_epochs_different_roots() {
        let output = create_test_shapley_output();

        let tree1 = ContributorRewardsMerkleTree::new(700, &output).unwrap();
        let tree2 = ContributorRewardsMerkleTree::new(701, &output).unwrap();

        let root1 = tree1.compute_root().unwrap();
        let root2 = tree2.compute_root().unwrap();

        // Different epochs should not affect root (only rewards matter)
        assert_eq!(root1, root2);
    }

    #[test]
    fn test_modified_reward_invalidates_proof() {
        let output = create_test_shapley_output();
        let tree = ContributorRewardsMerkleTree::new(800, &output).unwrap();
        let root = tree.compute_root().unwrap();

        // Get proof for first contributor
        let proof = tree.generate_proof(0).unwrap();
        let mut reward = tree.get_reward(0).unwrap().clone();

        // Modify reward proportion
        reward.proportion += 1;

        // Verify modified reward doesn't validate
        let leaf = borsh::to_vec(&reward).unwrap();
        let computed_root = proof.root_from_leaf(&leaf, Some(LEAF_PREFIX));

        assert_ne!(computed_root, root, "Modified reward should not validate");
    }

    #[test]
    fn test_merkle_root_with_many_contributors() {
        let mut output = ShapleyOutput::new();

        // Create 100 contributors using deterministic pubkeys
        for i in 0..100 {
            // Generate a deterministic pubkey for each contributor
            let mut bytes = [0u8; 32];
            bytes[0] = i as u8;
            bytes[1] = (i >> 8) as u8;
            let pubkey = Pubkey::new_from_array(bytes);
            output.insert(
                pubkey.to_string(),
                ShapleyValue {
                    value: (i as f64) * 10.0,
                    proportion: (i as f64) / 4950.0, // Sum of 0..100 = 4950
                },
            );
        }

        let tree = ContributorRewardsMerkleTree::new(900, &output).unwrap();
        assert_eq!(tree.len(), 100);

        let root = tree.compute_root().unwrap();

        // Verify a few random proofs
        for i in [0, 25, 50, 75, 99] {
            let proof = tree.generate_proof(i).unwrap();
            let reward = tree.get_reward(i).unwrap();
            let leaf = borsh::to_vec(reward).unwrap();
            let computed_root = proof.root_from_leaf(&leaf, Some(LEAF_PREFIX));

            assert_eq!(
                computed_root, root,
                "Proof verification failed for contributor at index {i}",
            );
        }
    }

    #[test]
    fn test_zero_value_rewards() {
        let mut output = ShapleyOutput::new();
        output.insert(
            "11111111111111111111111111111116".to_string(), // Zero pubkey
            ShapleyValue {
                value: 0.0,
                proportion: 0.0,
            },
        );
        output.insert(
            "11111111111111111111111111111117".to_string(), // NonZero pubkey
            ShapleyValue {
                value: 100.0,
                proportion: 1.0,
            },
        );

        let tree = ContributorRewardsMerkleTree::new(1000, &output).unwrap();
        let root = tree.compute_root().unwrap();

        // Both contributors should have valid proofs
        for i in 0..tree.len() {
            let proof = tree.generate_proof(i).unwrap();
            let reward = tree.get_reward(i).unwrap();
            let leaf = borsh::to_vec(reward).unwrap();
            let computed_root = proof.root_from_leaf(&leaf, Some(LEAF_PREFIX));

            assert_eq!(computed_root, root);
        }
    }

    #[test]
    fn test_negative_value_rewards() {
        let mut output = ShapleyOutput::new();
        output.insert(
            "11111111111111111111111111111118".to_string(), // Negative pubkey
            ShapleyValue {
                value: -50.0,
                proportion: -0.5,
            },
        );
        output.insert(
            "11111111111111111111111111111119".to_string(), // Positive pubkey
            ShapleyValue {
                value: 100.0,
                proportion: 1.0,
            },
        );

        let tree = ContributorRewardsMerkleTree::new(1100, &output).unwrap();
        let root = tree.compute_root().unwrap();

        // Negative values should still work
        for i in 0..tree.len() {
            let proof = tree.generate_proof(i).unwrap();
            let reward = tree.get_reward(i).unwrap();
            let leaf = borsh::to_vec(reward).unwrap();
            let computed_root = proof.root_from_leaf(&leaf, Some(LEAF_PREFIX));

            assert_eq!(computed_root, root);
        }
    }
}
