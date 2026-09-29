use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    DISCRIMINATOR_LEN, Discriminator, PrecomputedDiscriminator, types::StorageGap,
};
use solana_sdk::pubkey::Pubkey;

pub const PROGRAM_CONFIG_SEED_PREFIX: &[u8] = b"program_config";
pub const VALIDATOR_CLIENT_REWARDS_SEED_PREFIX: &[u8] = b"validator_client_rewards";

/// The most a manager may set a client's share to, in basis points.
pub const MAX_VALIDATOR_CLIENT_REWARDS_PROPORTION_BPS: u16 = 3_500;

pub fn find_program_config_address() -> (Pubkey, u8) {
    Pubkey::find_program_address(&[PROGRAM_CONFIG_SEED_PREFIX], &crate::feed_subscription::ID)
}

pub fn find_validator_client_rewards_address(client_id: u16) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            VALIDATOR_CLIENT_REWARDS_SEED_PREFIX,
            &client_id.to_le_bytes(),
        ],
        &crate::feed_subscription::ID,
    )
}

// ---------------------------------------------------------------------------
// ProgramConfig raw-byte parsing.
//
// Layout (ZeroCopy with 8-byte discriminator prefix):
//   [0..8)   discriminator
//   [8..16)  flags: Flags (u64, LE). Bit 0 is the pause bit.
//   ...      (remaining fields irrelevant for the CLI today)
// ---------------------------------------------------------------------------

const PROGRAM_CONFIG_DISCRIMINATOR: Discriminator<DISCRIMINATOR_LEN> =
    Discriminator::new_sha2(b"dz::account::program_config");

const PROGRAM_CONFIG_FLAGS_OFFSET: usize = DISCRIMINATOR_LEN;
const PROGRAM_CONFIG_FLAG_IS_PAUSED_BIT: u64 = 1 << 0;

/// Reads the pause bit from raw `ProgramConfig` account data. Returns `None` when the data is too
/// short or the discriminator does not match.
pub fn is_program_paused(data: &[u8]) -> Option<bool> {
    let discriminator = borsh::to_vec(&PROGRAM_CONFIG_DISCRIMINATOR).ok()?;
    if data.get(..DISCRIMINATOR_LEN)? != discriminator.as_slice() {
        return None;
    }
    let flags_bytes = data.get(PROGRAM_CONFIG_FLAGS_OFFSET..PROGRAM_CONFIG_FLAGS_OFFSET + 8)?;
    let flags = u64::from_le_bytes(<[u8; 8]>::try_from(flags_bytes).ok()?);
    Some(flags & PROGRAM_CONFIG_FLAG_IS_PAUSED_BIT != 0)
}

// ---------------------------------------------------------------------------
// ValidatorClientRewards: layout mirrored from the onchain `doublezero-feed-subscription` program
// (state module). Kept here because that program's repo depends on this one, so this crate cannot
// depend on it back. If the onchain layout changes, update both this file and the discriminator
// string together.
// ---------------------------------------------------------------------------

#[derive(Clone, Copy, Debug, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ValidatorClientRewards {
    pub client_id: u16,
    pub bump_seed: u8,
    _padding_0: [u8; 5],
    pub manager_key: Pubkey,
    pub short_description_bytes: [u8; 64],
    pub claim_holding_count: u32,
    _padding_1: [u8; 4],
    _gap: StorageGap<2>,
}

impl PrecomputedDiscriminator for ValidatorClientRewards {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::validator_client_rewards");
}

// Mirrors the onchain `assert!(size_of::<ValidatorClientRewards>() == 176)`.
const _: () = assert!(std::mem::size_of::<ValidatorClientRewards>() == 184 - DISCRIMINATOR_LEN);
const _: () = assert!(std::mem::offset_of!(ValidatorClientRewards, manager_key) == 8);

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_discriminators_match_the_onchain_idl() {
        assert_eq!(
            borsh::to_vec(&PROGRAM_CONFIG_DISCRIMINATOR).unwrap(),
            vec![207, 180, 133, 236, 48, 39, 241, 27]
        );
        assert_eq!(
            ValidatorClientRewards::discriminator_slice(),
            &[13, 180, 185, 41, 202, 245, 32, 184]
        );
    }

    #[test]
    fn test_is_program_paused_reads_bit_zero() {
        let mut data = borsh::to_vec(&PROGRAM_CONFIG_DISCRIMINATOR).unwrap();
        data.extend_from_slice(&0b0100_u64.to_le_bytes());
        assert_eq!(is_program_paused(&data), Some(false));

        data[DISCRIMINATOR_LEN..DISCRIMINATOR_LEN + 8].copy_from_slice(&0b0101_u64.to_le_bytes());
        assert_eq!(is_program_paused(&data), Some(true));
    }

    #[test]
    fn test_is_program_paused_refuses_other_accounts() {
        assert_eq!(is_program_paused(&[0; 4]), None);

        let mut data = ValidatorClientRewards::discriminator_slice().to_vec();
        data.extend_from_slice(&1_u64.to_le_bytes());
        assert_eq!(is_program_paused(&data), None);
    }

    #[test]
    fn test_addresses_are_derived_from_the_feed_program() {
        assert_eq!(
            find_validator_client_rewards_address(9).0,
            Pubkey::find_program_address(
                &[b"validator_client_rewards", &9_u16.to_le_bytes()],
                &crate::feed_subscription::ID,
            )
            .0
        );
        assert_ne!(
            find_validator_client_rewards_address(9).0,
            find_validator_client_rewards_address(10).0
        );
    }
}
