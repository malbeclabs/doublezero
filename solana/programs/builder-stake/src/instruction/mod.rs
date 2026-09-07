use borsh::{BorshDeserialize, BorshSerialize};
use solana_pubkey::Pubkey;

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ProgramConfiguration {
    Flag(ProgramFlagConfiguration),
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ProgramFlagConfiguration {
    IsPaused(bool),
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum BuilderStakeInstructionData {
    /// Create the program config and pause the program. Signed by the payer.
    InitializeProgram,

    /// Set the key allowed to configure the program. Signed by the program's upgrade authority,
    /// not by the current admin, so a lost admin key is recoverable through a redeploy authority
    /// rather than not at all.
    SetAdmin(Pubkey),

    /// Change a program setting. Signed by the admin.
    ConfigureProgram(ProgramConfiguration),

    /// Create a stake and its 2Z token account. Signed by the builder, which pays the rent.
    ///
    /// `stake_index` distinguishes a builder's stakes from each other and is part of the address,
    /// so creating the same index twice fails. `committed_rate_bits_per_sec` is the rate the feed
    /// backed by this stake may commit to, and it is what sizes the deposit.
    InitializeBuilderStake {
        stake_index: u64,
        committed_rate_bits_per_sec: u64,
    },

    /// Move 2Z from the builder's token account into the stake's. Signed by the builder, which
    /// signs the transfer as the source account's authority.
    Deposit { amount: u64 },
}
