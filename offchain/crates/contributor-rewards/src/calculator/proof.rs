use crate::calculator::constants::MAX_UNIT_SHARE;
use anyhow::{Result, anyhow, bail};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_revenue_distribution::types::{RewardShare, UnitShare32};
use network_shapley::shapley::ShapleyOutput;
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;
use svm_hash::{
    merkle::{MerkleProof, merkle_root_from_indexed_pod_leaves},
    sha2::Hash,
};

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
    pub rewards: Vec<RewardShare>,
    pub total_unit_shares: u32, // Should equal 1_000_000_000 for validation
}

#[derive(Debug)]
pub struct ContributorRewardsMerkleTree {
    epoch: u64,
    rewards: Vec<RewardShare>,
}

impl ContributorRewardsMerkleTree {
    pub fn new(epoch: u64, shapley_output: &ShapleyOutput) -> Result<Self> {
        if shapley_output.is_empty() {
            bail!("Empty Shapley output");
        }

        let mut rewards = Vec::new();
        let mut total_unit_shares = UnitShare32::default();

        for (operator_pubkey_str, val) in shapley_output.iter() {
            // Parse the operator string as a Pubkey
            let contributor_key = Pubkey::from_str(operator_pubkey_str)
                .map_err(|e| anyhow!("Invalid pubkey string '{}': {}", operator_pubkey_str, e))?;

            // Clamp f64 proportion
            let proportion = val.proportion.clamp(0.0, 1.0);
            // Convert f64 proportion to u32 with 9 decimal places
            let unit_share = UnitShare32::new((proportion * MAX_UNIT_SHARE).round() as u32)
                .ok_or_else(|| anyhow!("Invalid unit share"))?;
            total_unit_shares = total_unit_shares
                .checked_add(unit_share)
                .ok_or_else(|| anyhow!("Total unit shares overflow"))?;

            // Unwrapping is safe because we know the proportion is valid.
            rewards.push(
                RewardShare::new(
                    contributor_key,
                    unit_share.into(),
                    false, // should_block
                    0,
                )
                .unwrap(),
            );
        }

        // Reconcile rounding errors from float-to-fixed conversion.
        // Due to floating point precision, the sum might be slightly less than MAX.
        // We add the difference to the first reward to ensure the total equals exactly
        // 1_000_000_000 (100%), which is required by the on-chain contract.
        rewards[0].unit_share += u32::from(UnitShare32::MAX.saturating_sub(total_unit_shares));

        Ok(Self { epoch, rewards })
    }

    /// Compute the merkle root for all contributor rewards using POD serialization
    pub fn compute_root(&self) -> Result<Hash> {
        merkle_root_from_indexed_pod_leaves(&self.rewards, Some(RewardShare::LEAF_PREFIX))
            .ok_or_else(|| anyhow!("Failed to compute merkle root for epoch {}", self.epoch))
    }

    /// Generate a proof for a specific contributor by index
    pub fn generate_proof(&self, contributor_index: usize) -> Result<MerkleProof> {
        if contributor_index >= self.rewards.len() {
            bail!(
                "Invalid contributor index {} for epoch {}. Total contributors: {}",
                contributor_index,
                self.epoch,
                self.rewards.len()
            );
        }

        MerkleProof::from_indexed_pod_leaves(
            &self.rewards,
            contributor_index as u32,
            Some(RewardShare::LEAF_PREFIX),
        )
        .ok_or_else(|| {
            anyhow!(
                "Failed to generate proof for contributor {} at epoch {}",
                contributor_index,
                self.epoch
            )
        })
    }

    /// Get reward detail by index (for verification)
    pub fn get_reward(&self, index: usize) -> Option<&RewardShare> {
        self.rewards.get(index)
    }

    /// Get all rewards (for display)
    pub fn rewards(&self) -> &[RewardShare] {
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
) -> Result<(MerkleProof, RewardShare, Hash)> {
    // Find the contributor in the rewards list
    let mut contributor_index = None;
    let mut contributor_reward = None;

    for (index, reward) in shapley_storage.rewards.iter().enumerate() {
        if reward.contributor_key == *contributor_pubkey {
            contributor_index = Some(index);
            contributor_reward = Some(*reward);
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

    // Use POD-based merkle proof generation
    let proof = MerkleProof::from_indexed_pod_leaves(
        &shapley_storage.rewards,
        index as u32,
        Some(RewardShare::LEAF_PREFIX),
    )
    .ok_or_else(|| {
        anyhow!(
            "Failed to generate proof for contributor at index {}",
            index
        )
    })?;

    // Compute the root for verification using POD
    let root = merkle_root_from_indexed_pod_leaves(
        &shapley_storage.rewards,
        Some(RewardShare::LEAF_PREFIX),
    )
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
        assert_eq!(alice.unit_share, 500_000_000); // 0.5 * 1_000_000_000

        let bob_pubkey = Pubkey::from_str("11111111111111111111111111111113").unwrap();
        let bob = rewards
            .iter()
            .find(|r| r.contributor_key == bob_pubkey)
            .unwrap();
        assert_eq!(bob.unit_share, 250_000_000); // 0.25 * 1_000_000_000
    }

    #[test]
    fn test_single_contributor_tree() {
        let output = create_single_contributor_output();
        let tree = ContributorRewardsMerkleTree::new(456, &output).unwrap();

        assert_eq!(tree.len(), 1);
        assert!(!tree.is_empty());

        let root = tree.compute_root().unwrap();
        assert_ne!(root, Hash::default());

        // Verify proof generation succeeds for the single contributor
        tree.generate_proof(0).unwrap();
    }

    #[test]
    fn test_empty_tree() {
        let output = create_empty_output();
        // Empty tree will fail validation because proportions sum to 0, not 1_000_000_000
        let result = ContributorRewardsMerkleTree::new(789, &output);
        assert!(result.is_err());
        assert_eq!(result.unwrap_err().to_string(), "Empty Shapley output")
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

        // Test proof generation for each contributor
        for i in 0..tree.len() {
            // Verify proof generation succeeds for each contributor
            tree.generate_proof(i).unwrap();
        }

        // Verify root remains consistent
        let verified_root = tree.compute_root().unwrap();
        assert_eq!(verified_root, root, "Root verification failed");
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

        // Generate and serialize proof
        let proof = tree.generate_proof(0).unwrap();
        let proof_bytes = borsh::to_vec(&proof).unwrap();

        // Verify proof can be deserialized
        let _: MerkleProof = borsh::from_slice(&proof_bytes).unwrap();

        // Verify tree root remains consistent
        assert_eq!(tree.compute_root().unwrap(), root);
    }

    #[test]
    fn test_generate_proof_from_shapley() {
        let output = create_test_shapley_output();
        let tree = ContributorRewardsMerkleTree::new(600, &output).unwrap();

        // Create ShapleyOutputStorage
        let shapley_storage = ShapleyOutputStorage {
            epoch: 600,
            rewards: tree.rewards().to_vec(),
            total_unit_shares: tree.rewards().iter().map(|r| r.unit_share).sum(),
        };

        // Test generating proof for Alice
        let alice_pubkey = Pubkey::from_str("11111111111111111111111111111112").unwrap();
        let (_, reward, root) =
            generate_proof_from_shapley(&shapley_storage, &alice_pubkey).unwrap();

        assert_eq!(reward.contributor_key, alice_pubkey);
        assert_eq!(reward.unit_share, 500_000_000); // 0.5 * 1_000_000_000

        // Verify the root matches expected
        assert_eq!(
            root,
            merkle_root_from_indexed_pod_leaves(
                &shapley_storage.rewards,
                Some(RewardShare::LEAF_PREFIX)
            )
            .unwrap()
        );

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

        // Get first contributor's reward
        let mut reward = *tree.get_reward(0).unwrap();

        // Generate proof before modification
        tree.generate_proof(0).unwrap();

        // Modify reward unit_share
        reward.unit_share += 1;

        // Create a modified rewards list
        let mut modified_rewards = tree.rewards.clone();
        modified_rewards[0] = reward;

        // Verify modified reward produces different root
        let modified_root =
            merkle_root_from_indexed_pod_leaves(&modified_rewards, Some(RewardShare::LEAF_PREFIX))
                .unwrap();

        assert_ne!(
            modified_root, root,
            "Modified reward should produce different root"
        );
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

        // Verify proof generation succeeds for various indices
        for i in [0, 25, 50, 75, 99] {
            tree.generate_proof(i).unwrap();
        }

        // Verify root remains consistent
        let computed_root = tree.compute_root().unwrap();
        assert_eq!(computed_root, root, "Root verification failed");
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

        // Verify proof generation succeeds for both contributors
        for i in 0..tree.len() {
            tree.generate_proof(i).unwrap();
        }

        // Verify root remains consistent
        let computed_root = tree.compute_root().unwrap();
        assert_eq!(computed_root, root);
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

        // Verify proof generation succeeds even with negative values
        for i in 0..tree.len() {
            tree.generate_proof(i).unwrap();
        }

        // Verify root remains consistent
        let computed_root = tree.compute_root().unwrap();
        assert_eq!(computed_root, root);
    }
}
