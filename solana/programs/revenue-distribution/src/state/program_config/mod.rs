mod community_burn_rate;
mod distribution;
mod relay;

pub use community_burn_rate::*;
pub use distribution::*;
pub use relay::*;

//

use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, FlagsBitmap, StorageGap},
    Discriminator, PrecomputedDiscriminator,
};
use solana_pubkey::Pubkey;

use crate::types::{DoubleZeroEpoch, ValidatorFee};

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ProgramConfig {
    pub flags: Flags,

    pub next_dz_epoch: DoubleZeroEpoch,

    /// This seed will be used to sign for token transfers.
    pub bump_seed: u8,

    /// Cache this seed to validate token PDA address.
    pub reserve_2z_bump_seed: u8,
    _bump_seed_padding: [u8; 6],

    pub admin_key: Pubkey,

    /// This authority is the only authority that can post data relevant to fee calculations and
    /// contributor rewards.
    pub accountant_key: Pubkey,

    /// This authority is the only authority that can grant access to the DoubleZero Ledger network.
    pub contributor_manager_key: Pubkey,

    /// The program allowed to CPI to this program to withdraw SOL to swap for 2Z. The Revenue
    /// Distribution program trusts that the SOL/2Z Swap program will be depositing 2Z when it
    /// withdraws SOL.
    ///
    /// If the setup ever becomes trust-less, the procedure to swap SOL to 2Z will have to change.
    pub sol_2z_swap_program_id: Pubkey,

    pub distribution_parameters: DistributionParameters,

    pub relay_parameters: RelayParameters,

    /// 16 * 32 bytes of a storage gap in case more fields need to be added.
    _storage_gap: StorageGap<16>,
}

impl PrecomputedDiscriminator for ProgramConfig {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::program_config");
}

impl ProgramConfig {
    pub const SEED_PREFIX: &'static [u8] = b"program_config";

    pub const FLAG_IS_PAUSED_BIT: usize = 0;

    pub fn find_address() -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX], &crate::ID)
    }

    #[inline]
    pub fn flags_bitmap(&self) -> FlagsBitmap {
        FlagsBitmap::from_value(self.flags)
    }

    pub fn is_paused(&self) -> bool {
        self.flags_bitmap().get(Self::FLAG_IS_PAUSED_BIT)
    }

    pub fn set_is_paused(&mut self, should_pause: bool) {
        let mut flags_bitmap = self.flags_bitmap();
        flags_bitmap.set(Self::FLAG_IS_PAUSED_BIT, should_pause);
        self.flags = flags_bitmap.into_value();
    }

    pub fn checked_solana_validator_fee(&self) -> Option<ValidatorFee> {
        let fee = self.distribution_parameters.current_solana_validator_fee;

        if fee == Default::default() {
            None
        } else {
            Some(fee)
        }
    }
}

//

const _: () = assert!(
    size_of::<ProgramConfig>() == 1_000,
    "`ProgramConfig` size changed"
);
