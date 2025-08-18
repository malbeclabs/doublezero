use borsh::{BorshDeserialize, BorshSerialize};
use solana_sdk::pubkey::Pubkey;
use svm_hash::merkle::{merkle_root_from_byte_ref_leaves, MerkleProof};

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone)]
pub struct ComputedSolanaValidatorPayments {
    pub epoch: u64,
    pub payments: Vec<SolanaValidatorPayment>,
}

impl ComputedSolanaValidatorPayments {
    pub fn find_payment_proof(
        &self,
        validator_id: &Pubkey,
    ) -> Option<(&SolanaValidatorPayment, MerkleProof)> {
        let index = self
            .payments
            .iter()
            .position(|payment| &payment.node_id == validator_id)?;

        let solana_validator_payment_entry = &self.payments[index];

        let leaves = self.to_byte_leaves();

        let proof = MerkleProof::from_byte_ref_leaves(
            &leaves,
            index,
            Some(SolanaValidatorPayment::LEAF_PREFIX),
        )?;
        Some((solana_validator_payment_entry, proof))
    }

    pub fn merkle_root(&self) -> Option<svm_hash::sha2::Hash> {
        let leaves = self.to_byte_leaves();
        merkle_root_from_byte_ref_leaves(&leaves, Some(SolanaValidatorPayment::LEAF_PREFIX))
    }

    fn to_byte_leaves(&self) -> Vec<Vec<u8>> {
        self.payments
            .iter()
            .map(|payment| borsh::to_vec(&payment).unwrap())
            .collect()
    }
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, Copy, Default, PartialEq, Eq)]
pub struct SolanaValidatorPayment {
    pub node_id: Pubkey,
    pub amount: u64,
}

impl SolanaValidatorPayment {
    pub const LEAF_PREFIX: &'static [u8] = b"solana_validator_payment";

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
        let payments = ComputedSolanaValidatorPayments {
            epoch: 822,
            payments: vec![
                SolanaValidatorPayment {
                    node_id: Pubkey::new_unique(),
                    amount: 1343542456,
                },
                SolanaValidatorPayment {
                    node_id: Pubkey::new_unique(),
                    amount: 234234324,
                },
            ],
        };

        let leaf_prefix = Some(SolanaValidatorPayment::LEAF_PREFIX);
        let leaves = payments.to_byte_leaves();
        let leaves_ref: Vec<&[u8]> = leaves.iter().map(|v| v.as_slice()).collect();
        let root = payments.merkle_root().unwrap();

        let proof_left = payments
            .find_payment_proof(&payments.payments[0].node_id)
            .unwrap();

        let computed_proof_left = proof_left
            .1
            .root_from_byte_ref_leaf(&leaves_ref[0], Some(SolanaValidatorPayment::LEAF_PREFIX));

        let proof_right = payments
            .find_payment_proof(&payments.payments[1].node_id)
            .unwrap();

        let computed_proof_right = proof_right
            .1
            .root_from_byte_ref_leaf(&leaves_ref[1], Some(SolanaValidatorPayment::LEAF_PREFIX));

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

        assert_eq!(proof_left.0.node_id, payments.payments[0].node_id);
        assert_eq!(proof_right.0.node_id, payments.payments[1].node_id);

        Ok(())
    }
}
