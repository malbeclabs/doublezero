use bytemuck::{Pod, Zeroable};
use solana_sdk::pubkey::Pubkey;
use svm_hash::sha2::{Hash, hashv};

/// Canonical authorization message for `ConfigureFeedPublisherRewards`, byte for byte the
/// program's. The validator identity signs `to_hex_encoded()` with `solana sign-offchain-message`.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C)]
pub struct ConfigureFeedPublisherRewardsAuthMessage {
    pub program_id: Pubkey,
    pub node_id: Pubkey,
    pub rewards_token_owner_key: Pubkey,
    pub rewards_token_mint_key: Pubkey,
    pub deadline_slot: u64,
}

impl ConfigureFeedPublisherRewardsAuthMessage {
    pub const DOMAIN_TAG: &'static [u8] = b"dz::configure_feed_publisher_rewards::v1";

    pub const OFFCHAIN_MESSAGE_LENGTH: u16 = 64;

    /// The furthest ahead of the current slot the program accepts a deadline.
    pub const MAX_DEADLINE_SLOTS_AHEAD: u64 = 216_000;

    pub fn hash(&self) -> Hash {
        hashv(&[Self::DOMAIN_TAG, bytemuck::bytes_of(self)])
    }

    /// Lower-case hex of `hash()`, the exact string passed to `solana sign-offchain-message`.
    pub fn to_hex_encoded(&self) -> [u8; Self::OFFCHAIN_MESSAGE_LENGTH as usize] {
        let mut buf = [0; Self::OFFCHAIN_MESSAGE_LENGTH as usize];
        hex::encode_to_slice(self.hash().to_bytes(), &mut buf)
            .expect("hash is 32 bytes, buffer is 64 bytes");
        buf
    }
}

#[cfg(test)]
mod tests {
    use solana_offchain_message::OffchainMessage;
    use solana_sdk::signature::{Keypair, Signer};

    use super::*;

    #[test]
    fn test_hex_matches_the_program_for_fixed_terms() {
        let message = ConfigureFeedPublisherRewardsAuthMessage {
            program_id: Pubkey::new_from_array([1; 32]),
            node_id: Pubkey::new_from_array([2; 32]),
            rewards_token_owner_key: Pubkey::new_from_array([3; 32]),
            rewards_token_mint_key: Pubkey::new_from_array([4; 32]),
            deadline_slot: 0xdead_beef,
        };

        // What the feed program's own `to_hex_encoded` returns for these terms.
        assert_eq!(
            &message.to_hex_encoded(),
            b"6040372327b222e7b13d81f07efb1947de00bf3897bd820628eebaf31b441acc"
        );
    }

    // `OffchainMessage` is what `solana sign-offchain-message` signs with. The envelope below is
    // the one the program rebuilds from its own constants before it verifies.
    #[test]
    fn test_offchain_signature_verifies_over_the_program_envelope() {
        let keypair = Keypair::new();
        let hex = ConfigureFeedPublisherRewardsAuthMessage {
            program_id: *crate::feed_subscription::ID,
            node_id: keypair.pubkey(),
            rewards_token_owner_key: Pubkey::new_unique(),
            rewards_token_mint_key: Pubkey::new_unique(),
            deadline_slot: 12_345,
        }
        .to_hex_encoded();
        let signature = OffchainMessage::new(0, &hex)
            .unwrap()
            .sign(&keypair)
            .unwrap();

        let mut envelope = b"\xffsolana offchain".to_vec();
        // Header version.
        envelope.push(0);
        // Format: restricted ASCII.
        envelope.push(0);
        envelope.extend_from_slice(
            &ConfigureFeedPublisherRewardsAuthMessage::OFFCHAIN_MESSAGE_LENGTH.to_le_bytes(),
        );
        envelope.extend_from_slice(&hex);
        assert!(signature.verify(keypair.pubkey().as_ref(), &envelope));
    }
}
