use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator};
use solana_account_info::MAX_PERMITTED_DATA_INCREASE;
use solana_pubkey::Pubkey;

use crate::types::DoubleZeroEpoch;

pub const JOURNAL_ENTRIES_ABSOLUTE_MAX_LENGTH: u16 = 256;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct Journal {
    /// This seed will be used to sign for token transfers.
    pub bump_seed: u8,

    /// Cache this seed to validate token PDA address.
    pub token_2z_pda_bump_seed: u8,
    _padding: [u8; 6],

    pub total_sol_balance: u64,

    /// Based on interactions with the program to deposit 2Z, this is our
    /// expected balance. This balance may deviate from the actual balance in
    /// the 2Z Token account because folks may transfer tokens directly to
    /// that account (not intended). So if we wanted any recourse to do
    /// something with the excess amount in this token account, we can simply
    /// compute the difference between the token account balance and this.
    pub total_2z_balance: u64,

    pub swap_2z_destination_balance: u64,

    pub swapped_sol_amount: u64,

    pub next_dz_epoch_to_sweep_tokens: DoubleZeroEpoch,
}

impl PrecomputedDiscriminator for Journal {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::journal");
}

impl Journal {
    pub const SEED_PREFIX: &'static [u8] = b"journal";

    pub fn find_address() -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX], &crate::ID)
    }
}

//

const _: () = assert!(size_of::<Journal>() == 48, "`Journal` size changed");

const _: () = assert!(
    doublezero_program_tools::zero_copy::data_end::<Journal>() <= MAX_PERMITTED_DATA_INCREASE,
    "`Journal` total data length exceeds 10kb"
);
