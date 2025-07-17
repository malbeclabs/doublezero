mod community_burn_rate;

pub use community_burn_rate::*;

//

use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator};
use solana_pubkey::Pubkey;

use crate::types::{DoubleZeroEpoch, Flags, FlagsBitmap, ValidatorFee};

use super::StorageGap;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C)]
pub struct ProgramConfig {
    pub flags: Flags,

    pub next_dz_epoch: DoubleZeroEpoch,

    pub admin_key: Pubkey,

    /// This authority is the only authority that can post data relevant to fee calculations and
    /// contributor rewards.
    pub accountant_key: Pubkey,

    /// The program allowed to CPI to this program to withdraw SOL to swap for 2Z. The Revenue
    /// Distribution program trusts that the SOL/2Z Swap program will be depositing 2Z when it
    /// withdraws SOL.
    ///
    /// If the setup ever becomes trust-less, the procedure to swap SOL to 2Z will have to change.
    pub sol_2z_swap_program_id: Pubkey,

    /// Time to wait after the turn of the DZ epoch to perform any calculations.
    ///
    /// This field is not used for anything within the Revenue Distribution program because there is
    /// no way to enforce that calculations are performed at a specific time. But this field is
    /// stored here to act as a source-of-truth to inform the off-chain process (the accountant)
    /// how long it should wait after the new DZ epoch starts.
    ///
    /// This field also acts as an indication of whether the program config is initialized. If a
    /// grace period has not been configured, the program will not allow new Merkle roots (which
    /// are necessary for validators to pay their dues and contributors to claim rewards).
    pub calculation_grace_period_seconds: u32,

    pub community_burn_rate_parameters: CommunityBurnRateParameters,

    /// Proportion of Solana validator revenue DoubleZero collects to pay contributors. These fees
    /// are denominated in SOL, so this proportion represents a proportion of SOL rewards.
    pub current_solana_validator_fee: ValidatorFee,
    _current_solana_validator_fee_padding: [u8; 2],

    pub prepaid_connection_2z_activation_fee: u64,
    pub prepaid_connection_2z_cost_per_epoch: u64,

    pub contributor_reward_claim_relay_fee: u32,
    pub prepaid_user_disconnection_relay_fee: u32,

    /// 16 * 32 bytes of a storage gap in case more fields need to be added.
    _storage_gap: StorageGap<16>,
}

impl PrecomputedDiscriminator for ProgramConfig {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::program_config");
}

impl ProgramConfig {
    pub const SEED_PREFIX: &'static [u8] = b"program_config";

    pub const NUM_FLAGS: usize = u128::BITS as usize;

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
        if self.current_solana_validator_fee == Default::default() {
            None
        } else {
            Some(self.current_solana_validator_fee)
        }
    }
}
