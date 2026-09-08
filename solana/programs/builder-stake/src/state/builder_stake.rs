use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{types::StorageGap, Discriminator, PrecomputedDiscriminator};
use solana_pubkey::Pubkey;

/// A builder's 2Z bond against one feed.
///
/// A bond, not a deposit: it is returnable after the hold and forfeitable by slashing.
///
/// RFC-28 collateralizes each feed on its own bond: "A builder running a second feed posts a
/// second bond. Each feed is collateralized on its own, so slashing one never reaches another."
/// So the address carries `stake_index` as well as the builder, and a builder holds as many of
/// these as it runs feeds.
///
/// The tokens live in a separate token account owned by this PDA, not here.
/// [`bonded_2z_amount`] mirrors that balance so a reader needs one account rather than two;
/// the token account remains the authority on what is actually held.
///
/// [`bonded_2z_amount`]: Self::bonded_2z_amount
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct BuilderStake {
    pub builder: Pubkey,

    /// Which of this builder's stakes. Part of the address, so it is immutable.
    pub stake_index: u64,

    /// 2Z held in this stake's token account, in the mint's smallest unit.
    pub bonded_2z_amount: u64,

    /// What this stake has to hold for its committed rate.
    ///
    /// Follows the tier table while the stake is short, and stops moving once the stake is funded.
    /// Both halves matter. Freezing it at creation would let a builder pre-create stakes for the
    /// cost of rent and fund them after a repricing at the old price. Never freezing it would let
    /// a repricing make a builder short after it had already paid in full, which RFC-28's "fixed
    /// at the price prevailing when the tier is set" rules out.
    pub required_2z_amount: u64,

    /// The rate the feed backed by this stake may commit to, in bits per second. `u64::MAX` is the
    /// unmetered tier. Bits per second, not basis points: `bps` means basis points elsewhere in
    /// DoubleZero.
    pub committed_rate_bits_per_sec: u64,

    /// Unix seconds when the six-month minimum hold elapses, measured from the first bond posted.
    ///
    /// Always zero for now: nothing writes it. The hold and `Withdraw` are B3. The field is here
    /// already because this account is a fixed-size Pod struct, so adding one after deploy means
    /// migrating every account rather than editing a struct.
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

    /// Whether this stake holds what its committed rate requires. A stake that is not funded backs
    /// no feed: nothing mirrors it to the DZ ledger, so no feed can be created against it.
    pub fn is_funded(&self) -> bool {
        self.bonded_2z_amount >= self.required_2z_amount
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
