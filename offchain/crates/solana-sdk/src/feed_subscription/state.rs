use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator, types::StorageGap};
use solana_sdk::pubkey::Pubkey;

pub const PROGRAM_CONFIG_SEED_PREFIX: &[u8] = b"program_config";
pub const VALIDATOR_CLIENT_REWARDS_SEED_PREFIX: &[u8] = b"validator_client_rewards";
pub const FEED_DISTRIBUTION_JOURNAL_SEED_PREFIX: &[u8] = b"feed_distribution_journal";
pub const CLAIM_HOLDING_SEED_PREFIX: &[u8] = b"claim_holding";
pub const FEED_PUBLISHER_REWARDS_SEED_PREFIX: &[u8] = b"feed_publisher_rewards";
pub const FEED_PUBLISHERS_REWARD_TOKEN_SEED_PREFIX: &[u8] = b"feed_publishers_reward_token";

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

pub fn find_feed_distribution_journal_address(feed_distribution_key: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            FEED_DISTRIBUTION_JOURNAL_SEED_PREFIX,
            feed_distribution_key.as_ref(),
        ],
        &crate::feed_subscription::ID,
    )
}

/// A client team's token account for one feed month. The program only creates them in 2Z.
pub fn find_claim_holding_address(
    validator_client_rewards_key: &Pubkey,
    feed_distribution_key: &Pubkey,
    mint_key: &Pubkey,
) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            CLAIM_HOLDING_SEED_PREFIX,
            validator_client_rewards_key.as_ref(),
            feed_distribution_key.as_ref(),
            mint_key.as_ref(),
        ],
        &crate::feed_subscription::ID,
    )
}

pub fn find_feed_publisher_rewards_address(node_id: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[FEED_PUBLISHER_REWARDS_SEED_PREFIX, node_id.as_ref()],
        &crate::feed_subscription::ID,
    )
}

pub fn find_feed_publishers_reward_token_address(mint_key: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[FEED_PUBLISHERS_REWARD_TOKEN_SEED_PREFIX, mint_key.as_ref()],
        &crate::feed_subscription::ID,
    )
}

// ---------------------------------------------------------------------------
// ProgramConfig: only the leading flags and the oracle key, which is all the CLI reads. A checked
// read accepts a prefix of the account, so the rest of the onchain layout is not mirrored here.
// ---------------------------------------------------------------------------

const PROGRAM_CONFIG_FLAG_IS_PAUSED_BIT: u64 = 1 << 0;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Pod, Zeroable)]
#[repr(C)]
pub struct ProgramConfig {
    flags: u64,
    _admin_key: Pubkey,
    _repost_window_seconds: u32,
    _padding_0: [u8; 4],
    _min_burn_2z: u64,
    pub oracle_key: Pubkey,
}

// Mirrors the onchain `assert!(offset_of!(ProgramConfig, oracle_key) == 56)`.
const _: () = assert!(std::mem::offset_of!(ProgramConfig, oracle_key) == 56);

impl ProgramConfig {
    pub fn is_paused(&self) -> bool {
        self.flags & PROGRAM_CONFIG_FLAG_IS_PAUSED_BIT != 0
    }
}

impl PrecomputedDiscriminator for ProgramConfig {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::program_config");
}

// ---------------------------------------------------------------------------
// FeedDistribution: one account per feed and calendar month. Only the fields through the feed
// chain are mirrored, which is all the CLI reads.
// ---------------------------------------------------------------------------

/// The feed chain of a Solana feed's months.
pub const FEED_CHAIN_SOLANA: u16 = 1;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Pod, Zeroable)]
#[repr(C)]
pub struct FeedDistribution {
    pub feed_key: Pubkey,
    pub year: u16,
    pub month: u8,
    pub bump_seed: u8,
    pub publisher_rewards_proportion_bps: u16,
    pub payment_authority_bump_seed: u8,
    pub vault_usdc_ata_bump_seed: u8,
    pub collected_usdc_amount: u64,
    _flags: u64,
    pub feed_chain: u16,
    _padding_0: [u8; 6],
}

// Mirrors the onchain `assert!(offset_of!(FeedDistribution, feed_chain) == 56)`.
const _: () = assert!(std::mem::offset_of!(FeedDistribution, collected_usdc_amount) == 40);
const _: () = assert!(std::mem::offset_of!(FeedDistribution, feed_chain) == 56);

impl PrecomputedDiscriminator for FeedDistribution {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::feed_distribution::v2");
}

// ---------------------------------------------------------------------------
// FeedDistributionJournal: one account per posted month. Only the fields through the routed leaf
// count are mirrored, which is all the CLI reads.
// ---------------------------------------------------------------------------

#[derive(Clone, Copy, Debug, PartialEq, Eq, Pod, Zeroable)]
#[repr(C)]
pub struct FeedDistributionJournal {
    _publisher_rewards_merkle_root: [u8; 32],
    _validator_client_rewards_merkle_root: [u8; 32],
    pub total_publisher_weight: u64,
    pub total_validator_client_weight: u64,
    _padding_2: [u8; 8],
    pub validator_client_usdc_swapped: u64,
    pub validator_client_2z_converted: u64,
    pub validator_client_2z_paid: u64,
    pub rewards_posted_at: i64,
    _flags: u64,
    pub publisher_leaf_count: u32,
    pub accumulated_publisher_leaf_count: u32,
}

// Mirrors the onchain `assert!(offset_of!(FeedDistributionJournal, publisher_leaf_count) == 128)`.
const _: () = assert!(std::mem::offset_of!(FeedDistributionJournal, publisher_leaf_count) == 128);

impl FeedDistributionJournal {
    pub fn is_every_publisher_leaf_routed(&self) -> bool {
        self.accumulated_publisher_leaf_count == self.publisher_leaf_count
    }
}

impl PrecomputedDiscriminator for FeedDistributionJournal {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::feed_distribution_journal");
}

// ---------------------------------------------------------------------------
// FeedPublisherRewards: where one Solana publisher is paid. Only the node ID, owner and mint are
// mirrored, which is all the CLI reads.
// ---------------------------------------------------------------------------

#[derive(Clone, Copy, Debug, PartialEq, Eq, Pod, Zeroable)]
#[repr(C)]
pub struct FeedPublisherRewards {
    pub node_id: Pubkey,
    pub rewards_token_owner_key: Pubkey,
    pub rewards_token_mint_key: Pubkey,
}

impl PrecomputedDiscriminator for FeedPublisherRewards {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::feed_publisher_rewards");
}

// ---------------------------------------------------------------------------
// FeedPublishersRewardToken: only the mint and the flags, which is all the CLI reads.
// ---------------------------------------------------------------------------

const FEED_PUBLISHERS_REWARD_TOKEN_FLAG_IS_ENABLED_BIT: u64 = 1 << 0;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Pod, Zeroable)]
#[repr(C)]
pub struct FeedPublishersRewardToken {
    pub mint_key: Pubkey,
    flags: u64,
}

impl FeedPublishersRewardToken {
    pub fn is_enabled(&self) -> bool {
        self.flags & FEED_PUBLISHERS_REWARD_TOKEN_FLAG_IS_ENABLED_BIT != 0
    }
}

impl PrecomputedDiscriminator for FeedPublishersRewardToken {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::feed_publishers_reward_token");
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
    use doublezero_program_tools::{
        DISCRIMINATOR_LEN, zero_copy::checked_from_bytes_with_discriminator,
    };
    use solana_sdk::pubkey;

    use super::*;

    #[test]
    fn test_discriminators_match_the_onchain_idl() {
        assert_eq!(
            ProgramConfig::discriminator_slice(),
            &[207, 180, 133, 236, 48, 39, 241, 27]
        );
        assert_eq!(
            FeedDistribution::discriminator_slice(),
            &[56, 103, 126, 81, 85, 154, 72, 220]
        );
        assert_eq!(
            ValidatorClientRewards::discriminator_slice(),
            &[13, 180, 185, 41, 202, 245, 32, 184]
        );
        assert_eq!(
            FeedPublishersRewardToken::discriminator_slice(),
            &[104, 234, 187, 195, 22, 11, 249, 89]
        );
        assert_eq!(
            FeedDistributionJournal::discriminator_slice(),
            &[197, 183, 180, 93, 4, 213, 37, 189]
        );
        assert_eq!(
            FeedPublisherRewards::discriminator_slice(),
            &[186, 50, 68, 53, 54, 222, 177, 9]
        );
    }

    #[test]
    fn test_program_config_reads_the_pause_bit_and_oracle_key_from_a_prefix() {
        // The program's 232-byte account, with the oracle key at byte 56.
        let account_data = |flags: u64| {
            let mut data = vec![0; DISCRIMINATOR_LEN + 232];
            data[..DISCRIMINATOR_LEN].copy_from_slice(ProgramConfig::discriminator_slice());
            data[DISCRIMINATOR_LEN..][..8].copy_from_slice(&flags.to_le_bytes());
            data[DISCRIMINATOR_LEN + 56..][..32].copy_from_slice(&[5; 32]);
            data
        };

        let running_data = account_data(0b0100);
        let (config, _) =
            checked_from_bytes_with_discriminator::<ProgramConfig>(&running_data).unwrap();
        assert!(!config.is_paused());
        assert_eq!(config.oracle_key, Pubkey::new_from_array([5; 32]));

        let paused_data = account_data(0b0101);
        let (config, _) =
            checked_from_bytes_with_discriminator::<ProgramConfig>(&paused_data).unwrap();
        assert!(config.is_paused());
    }

    #[test]
    fn test_feed_distribution_reads_the_feed_and_month_from_a_prefix() {
        // The program's 120-byte account: the feed key, then the year at byte 32, the month at
        // byte 34, the collected USDC at byte 40 and the feed chain at byte 56.
        let mut data = vec![0; DISCRIMINATOR_LEN + 120];
        data[..DISCRIMINATOR_LEN].copy_from_slice(FeedDistribution::discriminator_slice());
        data[DISCRIMINATOR_LEN..][..32].copy_from_slice(&[7; 32]);
        data[DISCRIMINATOR_LEN + 32..][..2].copy_from_slice(&2_026_u16.to_le_bytes());
        data[DISCRIMINATOR_LEN + 34] = 3;
        data[DISCRIMINATOR_LEN + 40..][..8].copy_from_slice(&1_250_000_u64.to_le_bytes());
        data[DISCRIMINATOR_LEN + 56..][..2].copy_from_slice(&FEED_CHAIN_SOLANA.to_le_bytes());

        let (feed_distribution, _) =
            checked_from_bytes_with_discriminator::<FeedDistribution>(&data).unwrap();
        assert_eq!(feed_distribution.feed_key, Pubkey::new_from_array([7; 32]));
        assert_eq!(feed_distribution.year, 2_026);
        assert_eq!(feed_distribution.month, 3);
        assert_eq!(feed_distribution.collected_usdc_amount, 1_250_000);
        assert_eq!(feed_distribution.feed_chain, FEED_CHAIN_SOLANA);
    }

    #[test]
    fn test_feed_distribution_journal_reads_the_leaf_counts_from_a_prefix() {
        // The program's 488-byte account, with the leaf count at byte 128 and the routed leaf
        // count at byte 132.
        let mut data = vec![0; DISCRIMINATOR_LEN + 488];
        data[..DISCRIMINATOR_LEN].copy_from_slice(FeedDistributionJournal::discriminator_slice());
        data[DISCRIMINATOR_LEN + 128..][..4].copy_from_slice(&700_u32.to_le_bytes());
        data[DISCRIMINATOR_LEN + 132..][..4].copy_from_slice(&699_u32.to_le_bytes());

        let (journal, _) =
            checked_from_bytes_with_discriminator::<FeedDistributionJournal>(&data).unwrap();
        assert_eq!(journal.publisher_leaf_count, 700);
        assert!(!journal.is_every_publisher_leaf_routed());

        data[DISCRIMINATOR_LEN + 132..][..4].copy_from_slice(&700_u32.to_le_bytes());
        let (journal, _) =
            checked_from_bytes_with_discriminator::<FeedDistributionJournal>(&data).unwrap();
        assert!(journal.is_every_publisher_leaf_routed());
    }

    #[test]
    fn test_claim_holding_address_matches_the_program() {
        // What the program's own `find_claim_holding_address` returns for these keys, under the
        // default program ID.
        assert_eq!(
            find_claim_holding_address(
                &find_validator_client_rewards_address(9).0,
                &Pubkey::new_from_array([3; 32]),
                &Pubkey::new_from_array([6; 32]),
            ),
            (pubkey!("C8u7uYbdGgosLE3CLG8j5CDRcm3mVTnaikbdKdj78Gvg"), 255)
        );
    }

    #[test]
    fn test_feed_publishers_reward_token_reads_the_enabled_bit() {
        // The program's layout starts with the mint, then the flags with the enabled bit at 0.
        let account_data = |flags: u64| {
            let mut data = FeedPublishersRewardToken::discriminator_slice().to_vec();
            data.extend_from_slice(&[4; 32]);
            data.extend_from_slice(&flags.to_le_bytes());
            data
        };

        let disabled_data = account_data(0b0010);
        let (reward_token, _) =
            checked_from_bytes_with_discriminator::<FeedPublishersRewardToken>(&disabled_data)
                .unwrap();
        assert_eq!(reward_token.mint_key, Pubkey::new_from_array([4; 32]));
        assert!(!reward_token.is_enabled());

        let enabled_data = account_data(0b0011);
        let (reward_token, _) =
            checked_from_bytes_with_discriminator::<FeedPublishersRewardToken>(&enabled_data)
                .unwrap();
        assert!(reward_token.is_enabled());
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
