pub mod builders;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_pubkey::Pubkey;

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ProgramConfiguration {
    Flag(ProgramFlagConfiguration),

    /// The 2Z a bond costs at each rate tier. All three must be non-zero and must not decrease
    /// as the rate rises.
    TierParameters {
        up_to_1gbps_2z_amount: u64,
        up_to_5gbps_2z_amount: u64,
        unmetered_2z_amount: u64,
    },
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ProgramFlagConfiguration {
    IsPaused(bool),
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum BuilderBondInstructionData {
    /// Create the program config and pause the program. Signed by the payer.
    InitializeProgram,

    /// Set the key allowed to configure the program. Signed by the program's upgrade authority,
    /// not by the current admin, so a lost admin key is recoverable through a redeploy authority
    /// rather than not at all.
    SetAdmin(Pubkey),

    /// Change a program setting. Signed by the admin.
    ConfigureProgram(ProgramConfiguration),

    /// Create a bond and its 2Z token account. Signed by the builder, which pays the rent.
    ///
    /// `bond_index` distinguishes a builder's bonds from each other and is part of the address,
    /// so creating the same index twice fails. `committed_rate_bits_per_sec` is the rate the feed
    /// backed by this bond may commit to. It selects the tier, and the tier sets the bond this
    /// bond has to hold, pinned on the account as `required_2z_amount`.
    InitializeBuilderBond {
        bond_index: u64,
        committed_rate_bits_per_sec: u64,
    },

    /// Move 2Z from the builder's token account into the bond's. Signed by the builder, which
    /// signs the transfer as the source token account's authority.
    ///
    /// The first bond starts the six-month hold. Later ones top the bond up and do not restart
    /// it, so a repricing that forces a top-up cannot extend the hold.
    PostBond { amount: u64 },

    /// Return 2Z the bond holds above its requirement to a token account the builder names.
    /// Signed by the builder. Refused until the hold elapses, and never below the requirement.
    Withdraw { amount: u64 },

    /// Move this bond's hold expiry, so a devnet demo can show a withdrawal without waiting six
    /// months. Signed by the admin.
    ///
    /// Present in every build on purpose. Putting it behind `#[cfg]` would give the development
    /// and mainnet binaries different instruction encodings for the same bytes, which is a worse
    /// failure than an instruction that refuses to run. The mainnet build compiles the refusal in.
    SetHoldExpiry { hold_expires_at: i64 },
}
