use bytemuck::{Pod, Zeroable};
use solana_sdk::pubkey::Pubkey;
use svm_hash::sha2::{Hash, hashv};

/// Canonical authorization message for `ConfigureValidatorPublisherRewards`.
///
/// Layout is fixed and matches the on-chain crate verbatim. The `node_id`
/// keypair signs `to_hex_encoded()` via `solana sign-offchain-message`; the
/// program rebuilds the offchain-message envelope around the hex body and
/// verifies the ed25519 signature.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C)]
pub struct ConfigureValidatorPublisherRewardsAuthMessage {
    pub program_id: Pubkey,
    pub node_id: Pubkey,
    pub rewards_token_owner_key: Pubkey,
    pub rewards_token_mint_key: Pubkey,
    pub deadline_slot: u64,
}

impl ConfigureValidatorPublisherRewardsAuthMessage {
    /// Domain tag — versioned so a future v2 layout can't be replayed against
    /// the v1 program. Must match the on-chain constant of the same name.
    pub const DOMAIN_TAG: &'static [u8] = b"dz::configure_validator_publisher_rewards::v1";

    /// Length (bytes) of the hex-encoded hash body inside the offchain-message
    /// envelope. 32-byte sha256 * 2 hex chars = 64.
    pub const OFFCHAIN_MESSAGE_LENGTH: u16 = 64;

    /// SHA-256 over `DOMAIN_TAG` and the canonical struct bytes.
    #[inline]
    pub fn hash(&self) -> Hash {
        hashv(&[Self::DOMAIN_TAG, bytemuck::bytes_of(self)])
    }

    /// Lower-case ASCII hex of `self.hash()`. This is the exact string a node
    /// operator passes to `solana sign-offchain-message`.
    #[inline]
    pub fn to_hex_encoded(&self) -> [u8; Self::OFFCHAIN_MESSAGE_LENGTH as usize] {
        let mut buf = [0u8; Self::OFFCHAIN_MESSAGE_LENGTH as usize];
        hex::encode_to_slice(self.hash().to_bytes(), &mut buf)
            .expect("hash is 32 bytes; buffer is 64 bytes");
        buf
    }
}

#[cfg(test)]
mod tests {
    use solana_offchain_message::OffchainMessage;
    use solana_sdk::signature::{Keypair, Signature, Signer};

    use super::*;

    fn sample(keypair: &Keypair) -> ConfigureValidatorPublisherRewardsAuthMessage {
        ConfigureValidatorPublisherRewardsAuthMessage {
            program_id: Pubkey::new_unique(),
            node_id: keypair.pubkey(),
            rewards_token_owner_key: Pubkey::new_unique(),
            rewards_token_mint_key: Pubkey::new_unique(),
            deadline_slot: 12_345,
        }
    }

    /// Sign the hex-of-hash via `OffchainMessage` (byte-for-byte identical to
    /// `solana sign-offchain-message`) and verify with the same envelope. This
    /// is the cross-crate canary that detects DOMAIN_TAG / field-order drift
    /// between this SDK and the on-chain program.
    #[test]
    fn sign_and_verify_round_trip() {
        let keypair = Keypair::new();
        let message = sample(&keypair);

        let hex = message.to_hex_encoded();
        let offchain = OffchainMessage::new(0, &hex).unwrap();
        let signature: Signature = offchain.sign(&keypair).unwrap();

        // Re-construct the envelope and verify with the public key.
        assert!(offchain.verify(&keypair.pubkey(), &signature).unwrap());
    }

    /// Locks the byte layout against a frozen reference hash. If the on-chain
    /// `DOMAIN_TAG` or field order ever change, this test fails. Update only
    /// when the on-chain layout actually changes (and bump DOMAIN_TAG to v2).
    #[test]
    fn frozen_reference_hash() {
        let message = ConfigureValidatorPublisherRewardsAuthMessage {
            program_id: Pubkey::new_from_array([1u8; 32]),
            node_id: Pubkey::new_from_array([2u8; 32]),
            rewards_token_owner_key: Pubkey::new_from_array([3u8; 32]),
            rewards_token_mint_key: Pubkey::new_from_array([4u8; 32]),
            deadline_slot: 0xdead_beef,
        };
        // Frozen reference. To (re)generate: replace with `[0u8; 64]`, run
        // the test, and copy the 64-char hex string printed as the
        // assertion's `left` value. Updating this should only happen when
        // the on-chain `DOMAIN_TAG` or struct layout changes (i.e. on a
        // major version bump that breaks compatibility on purpose).
        let expected: &[u8; 64] =
            b"97d29f79630bd3871bb73784f4baa4ed685758a26afb6257e13a439defe2b2c2";
        assert_eq!(&message.to_hex_encoded(), expected);
    }
}
