use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{types::StorageGap, Discriminator, PrecomputedDiscriminator};
use solana_pubkey::Pubkey;

/// A builder's 2Z security deposit against one feed.
///
/// RFC-28 collateralizes each feed on its own deposit: "A builder running a second feed posts a
/// second deposit. Each feed is collateralized on its own, so slashing one never reaches another."
/// So the address carries `stake_index` as well as the builder, and a builder holds as many of
/// these as it runs feeds.
///
/// The tokens live in a separate token account owned by this PDA, not here.
/// [`deposited_2z_amount`] mirrors that balance so a reader needs one account rather than two;
/// the token account remains the authority on what is actually held.
///
/// [`deposited_2z_amount`]: Self::deposited_2z_amount
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct BuilderStake {
    pub builder: Pubkey,

    /// Which of this builder's stakes. Part of the address, so it is immutable.
    pub stake_index: u64,

    /// 2Z held in this stake's token account, in the mint's smallest unit.
    pub deposited_2z_amount: u64,

    /// The rate the feed backed by this stake may commit to, in bits per second. `u64::MAX` is the
    /// unmetered tier. Bits per second, not basis points: `bps` means basis points elsewhere in
    /// DoubleZero.
    pub committed_rate_bits_per_sec: u64,

    /// Unix seconds when the six-month minimum hold elapses, measured from the first deposit.
    /// Zero until the first deposit sets it.
    pub hold_expires_at: i64,

    /// Signs token transfers out of this stake's token account.
    pub bump_seed: u8,

    /// Cached to validate the token account address without re-deriving it.
    pub token_account_bump_seed: u8,

    _padding: [u8; 6],

    _storage_gap: StorageGap<2>,
}

impl PrecomputedDiscriminator for BuilderStake {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::builder_stake");
}

impl BuilderStake {
    pub const SEED_PREFIX: &'static [u8] = b"builder_stake";

    pub fn find_address(builder: &Pubkey, stake_index: u64) -> (Pubkey, u8) {
        Pubkey::find_program_address(
            &[
                Self::SEED_PREFIX,
                builder.as_ref(),
                &stake_index.to_le_bytes(),
            ],
            &crate::ID,
        )
    }

    pub fn checked_address(builder: &Pubkey, stake_index: u64, bump_seed: u8) -> Option<Pubkey> {
        Pubkey::create_program_address(
            &[
                Self::SEED_PREFIX,
                builder.as_ref(),
                &stake_index.to_le_bytes(),
                &[bump_seed],
            ],
            &crate::ID,
        )
        .ok()
    }
}
