use borsh::{BorshDeserialize, BorshSerialize};
use solana_sdk::{hash::Hash, pubkey::Pubkey};
use svm_hash::merkle::{MerkleProof, merkle_root_from_indexed_byte_ref_leaves};

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub struct ComputedSolanaValidatorDebts {
    pub blockhash: Hash,
    pub epoch: u64,
    pub debts: Vec<ComputedSolanaValidatorDebt>,
}

impl ComputedSolanaValidatorDebts {
    pub fn find_debt_proof(
        &self,
        validator_id: &Pubkey,
    ) -> Option<(&ComputedSolanaValidatorDebt, MerkleProof)> {
        let index = self
            .debts
            .iter()
            .position(|debt| &debt.node_id == validator_id)?;

        let solana_validator_debt_entry = &self.debts[index];
        let leaves = self.to_byte_leaves();
        let proof = MerkleProof::from_indexed_byte_ref_leaves(
            &leaves,
            index as u32,
            Some(ComputedSolanaValidatorDebt::LEAF_PREFIX),
        )?;
        Some((solana_validator_debt_entry, proof))
    }

    pub fn merkle_root(&self) -> Option<svm_hash::sha2::Hash> {
        let leaves = self.to_byte_leaves();
        merkle_root_from_indexed_byte_ref_leaves(
            &leaves,
            Some(ComputedSolanaValidatorDebt::LEAF_PREFIX),
        )
    }

    fn to_byte_leaves(&self) -> Vec<Vec<u8>> {
        self.debts
            .iter()
            .map(|debt| borsh::to_vec(&debt).unwrap())
            .collect()
    }
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, Copy, Default, PartialEq, Eq)]
pub struct ComputedSolanaValidatorDebt {
    pub node_id: Pubkey,
    pub amount: u64,
}

impl ComputedSolanaValidatorDebt {
    pub const LEAF_PREFIX: &'static [u8] = b"solana_validator_debt";

    pub fn merkle_root(&self, proof: MerkleProof) -> svm_hash::sha2::Hash {
        let mut leaf = [0; 40];

        // This is infallible because we know the size of the struct.
        borsh::to_writer(&mut leaf[..], &self).unwrap();

        proof.root_from_leaf(&leaf, Some(Self::LEAF_PREFIX))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use anyhow::Result;

    #[test]
    fn test_add_rewards_to_tree() -> Result<()> {
        let debts = ComputedSolanaValidatorDebts {
            blockhash: Hash::new_unique(),
            epoch: 822,
            debts: vec![
                ComputedSolanaValidatorDebt {
                    node_id: Pubkey::new_unique(),
                    amount: 1343542456,
                },
                ComputedSolanaValidatorDebt {
                    node_id: Pubkey::new_unique(),
                    amount: 234234324,
                },
            ],
        };

        let leaf_prefix = Some(ComputedSolanaValidatorDebt::LEAF_PREFIX);
        let leaves = debts.to_byte_leaves();
        let leaves_ref: Vec<&[u8]> = leaves.iter().map(|v| v.as_slice()).collect();
        let root = debts.merkle_root().unwrap();

        let proof_left = debts.find_debt_proof(&debts.debts[0].node_id).unwrap();

        let computed_proof_left = proof_left.1.root_from_byte_ref_leaf(
            &leaves_ref[0],
            Some(ComputedSolanaValidatorDebt::LEAF_PREFIX),
        );

        let proof_right = debts.find_debt_proof(&debts.debts[1].node_id).unwrap();

        let computed_proof_right = proof_right.1.root_from_byte_ref_leaf(
            &leaves_ref[1],
            Some(ComputedSolanaValidatorDebt::LEAF_PREFIX),
        );

        assert_eq!(
            proof_left.1.root_from_leaf(leaves_ref[0], leaf_prefix),
            computed_proof_left
        );
        assert_eq!(
            proof_left.1.root_from_leaf(leaves_ref[0], leaf_prefix),
            root
        );

        assert_eq!(
            proof_right.1.root_from_leaf(leaves_ref[1], leaf_prefix),
            computed_proof_right
        );
        assert_eq!(
            proof_right.1.root_from_leaf(leaves_ref[1], leaf_prefix),
            root
        );

        assert_eq!(proof_left.0.node_id, debts.debts[0].node_id);
        assert_eq!(proof_right.0.node_id, debts.debts[1].node_id);

        Ok(())
    }
}
