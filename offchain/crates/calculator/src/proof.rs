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
