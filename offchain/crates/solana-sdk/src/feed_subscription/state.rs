use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator, types::StorageGap};
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
// ProgramConfig: only the leading flags, which is all the CLI reads. A checked read accepts a
// prefix of the account, so the rest of the onchain layout is not mirrored here.
// ---------------------------------------------------------------------------

const PROGRAM_CONFIG_FLAG_IS_PAUSED_BIT: u64 = 1 << 0;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Pod, Zeroable)]
#[repr(C)]
pub struct ProgramConfig {
    flags: u64,
}

impl ProgramConfig {
    pub fn is_paused(&self) -> bool {
        self.flags & PROGRAM_CONFIG_FLAG_IS_PAUSED_BIT != 0
    }
}

impl PrecomputedDiscriminator for ProgramConfig {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::program_config");
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
const _: () = assert!(std::mem::size_of::<ValidatorClientRewards>() == 176);
const _: () = assert!(std::mem::offset_of!(ValidatorClientRewards, manager_key) == 8);

#[cfg(test)]
mod tests {
    use doublezero_program_tools::zero_copy::checked_from_bytes_with_discriminator;

    use super::*;

    #[test]
    fn test_discriminators_match_the_onchain_idl() {
        assert_eq!(
            ProgramConfig::discriminator_slice(),
            &[207, 180, 133, 236, 48, 39, 241, 27]
        );
        assert_eq!(
            ValidatorClientRewards::discriminator_slice(),
            &[13, 180, 185, 41, 202, 245, 32, 184]
        );
    }

    #[test]
    fn test_program_config_reads_pause_bit_from_a_prefix() {
        let account_data = |flags: u64| {
            let mut data = ProgramConfig::discriminator_slice().to_vec();
            data.extend_from_slice(&flags.to_le_bytes());
            data.extend_from_slice(&[0; 32]);
            data
        };

        let (config, _) =
            checked_from_bytes_with_discriminator::<ProgramConfig>(&account_data(0b0100)).unwrap();
        assert!(!config.is_paused());

        let (config, _) =
            checked_from_bytes_with_discriminator::<ProgramConfig>(&account_data(0b0101)).unwrap();
        assert!(config.is_paused());
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
    }
}
