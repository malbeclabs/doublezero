use anyhow::{Result, anyhow};
use borsh::{BorshDeserialize, BorshSerialize};
use network_shapley::shapley::ShapleyOutput;
use svm_hash::{
    merkle::{MerkleProof, merkle_root_from_byte_ref_leaves},
    sha2::Hash,
};

const LEAF_PREFIX: &[u8] = b"dz_contributor_rewards";

#[derive(Debug, Clone, BorshDeserialize, BorshSerialize)]
pub struct ContributorRewardDetail {
    pub operator: String,
    pub value: f64,
    pub proportion: f64,
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

#[derive(Debug, Clone, BorshDeserialize, BorshSerialize)]
pub struct ContributorRewardProof {
    pub epoch: u64,
    pub contributor: String,
    pub reward: ContributorRewardDetail,
    pub proof_bytes: Vec<u8>, // Store serialized MerkleProof
    pub index: u32,
}

#[derive(Debug)]
pub struct ContributorRewardsMerkleTree {
    epoch: u64,
    rewards: Vec<ContributorRewardDetail>,
    leaves: Vec<Vec<u8>>,
}

impl ContributorRewardsMerkleTree {
    pub fn new(epoch: u64, shapley_output: &ShapleyOutput) -> Result<Self> {
        let rewards: Vec<ContributorRewardDetail> = shapley_output
            .iter()
            .map(|(operator, val)| ContributorRewardDetail {
                operator: operator.to_string(),
                value: val.value,
                proportion: val.proportion,
            })
            .collect();

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

        MerkleProof::from_byte_ref_leaves(&self.leaves, contributor_index, Some(LEAF_PREFIX))
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

// Convenience functions for direct usage
pub fn compute_rewards_merkle_root(epoch: u64, shapley_output: &ShapleyOutput) -> Result<Hash> {
    let tree = ContributorRewardsMerkleTree::new(epoch, shapley_output)?;
    tree.compute_root()
}

pub fn generate_rewards_proof(
    epoch: u64,
    shapley_output: &ShapleyOutput,
    contributor_index: usize,
) -> Result<(MerkleProof, ContributorRewardDetail)> {
    let tree = ContributorRewardsMerkleTree::new(epoch, shapley_output)?;
    let proof = tree.generate_proof(contributor_index)?;
    let reward = tree
        .get_reward(contributor_index)
        .ok_or_else(|| anyhow!("Reward not found at index {}", contributor_index))?
        .clone();

    Ok((proof, reward))
}

#[cfg(test)]
mod tests {
    use super::*;
    use network_shapley::shapley::ShapleyValue;

    fn create_test_shapley_output() -> ShapleyOutput {
        let mut output = ShapleyOutput::new();
        output.insert(
            "Alice".to_string(),
            ShapleyValue {
                value: 100.0,
                proportion: 0.5,
            },
        );
        output.insert(
            "Bob".to_string(),
            ShapleyValue {
                value: 50.0,
                proportion: 0.25,
            },
        );
        output.insert(
            "Charlie".to_string(),
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
            "Solo".to_string(),
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

        // Find each contributor in rewards (order may vary due to HashMap)
        let alice = rewards.iter().find(|r| r.operator == "Alice").unwrap();
        assert_eq!(alice.value, 100.0);
        assert_eq!(alice.proportion, 0.5);

        let bob = rewards.iter().find(|r| r.operator == "Bob").unwrap();
        assert_eq!(bob.value, 50.0);
        assert_eq!(bob.proportion, 0.25);
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
        let tree = ContributorRewardsMerkleTree::new(789, &output).unwrap();

        assert_eq!(tree.len(), 0);
        assert!(tree.is_empty());

        // Empty tree cannot compute a root (returns error)
        let result = tree.compute_root();
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("Failed to compute merkle root")
        );
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
    fn test_contributor_reward_proof_struct() {
        let output = create_test_shapley_output();
        let tree = ContributorRewardsMerkleTree::new(500, &output).unwrap();

        let proof = tree.generate_proof(0).unwrap();
        let reward = tree.get_reward(0).unwrap().clone();
        let proof_bytes = borsh::to_vec(&proof).unwrap();

        // Create ContributorRewardProof
        let contributor_proof = ContributorRewardProof {
            epoch: 500,
            contributor: reward.operator.clone(),
            reward: reward.clone(),
            proof_bytes,
            index: 0,
        };

        // Serialize and deserialize
        let serialized = borsh::to_vec(&contributor_proof).unwrap();
        let deserialized: ContributorRewardProof = borsh::from_slice(&serialized).unwrap();

        assert_eq!(deserialized.epoch, 500);
        assert_eq!(deserialized.contributor, reward.operator);
        assert_eq!(deserialized.reward.value, reward.value);
        assert_eq!(deserialized.reward.proportion, reward.proportion);
        assert_eq!(deserialized.index, 0);
    }

    #[test]
    fn test_convenience_functions() {
        let output = create_test_shapley_output();

        // Test compute_rewards_merkle_root
        let root = compute_rewards_merkle_root(600, &output).unwrap();
        assert_ne!(root, Hash::default());

        // Test generate_rewards_proof
        let (proof, reward) = generate_rewards_proof(600, &output, 0).unwrap();

        // Verify the proof
        let leaf = borsh::to_vec(&reward).unwrap();
        let computed_root = proof.root_from_leaf(&leaf, Some(LEAF_PREFIX));
        assert_eq!(computed_root, root);
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

        // Modify reward
        reward.value += 1.0;

        // Verify modified reward doesn't validate
        let leaf = borsh::to_vec(&reward).unwrap();
        let computed_root = proof.root_from_leaf(&leaf, Some(LEAF_PREFIX));

        assert_ne!(computed_root, root, "Modified reward should not validate");
    }

    #[test]
    fn test_merkle_root_with_many_contributors() {
        let mut output = ShapleyOutput::new();

        // Create 100 contributors
        for i in 0..100 {
            output.insert(
                format!("Contributor{i}"),
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
            "Zero".to_string(),
            ShapleyValue {
                value: 0.0,
                proportion: 0.0,
            },
        );
        output.insert(
            "NonZero".to_string(),
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
            "Negative".to_string(),
            ShapleyValue {
                value: -50.0,
                proportion: -0.5,
            },
        );
        output.insert(
            "Positive".to_string(),
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
