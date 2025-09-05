mod community_burn_rate;
mod distribution;
mod relay;

pub use community_burn_rate::*;
pub use distribution::*;
pub use relay::*;

//

use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, StorageGap},
    Discriminator, PrecomputedDiscriminator,
};
use solana_pubkey::Pubkey;

use crate::types::{DoubleZeroEpoch, EpochDuration};

use super::checked_2z_token_pda_address;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ProgramConfig {
    pub flags: Flags,

    pub next_dz_epoch: DoubleZeroEpoch,

    /// This seed will be used to sign for token transfers.
    pub bump_seed: u8,

    /// Cache this seed to validate token PDA address.
    pub reserve_2z_bump_seed: u8,

    /// This seed will be used to sign for token transfers from the swap
    /// destination token account.
    pub swap_authority_bump_seed: u8,

    /// Cache this seed to validate token PDA address.
    pub swap_destination_2z_bump_seed: u8,

    /// Cache this seed to validate withdraw SOL authority address, which is
    /// the required signer of [Self::sol_2z_swap_program_id] to withdraw SOL.
    pub withdraw_sol_authority_bump_seed: u8,

    _padding: [u8; 3],

    pub admin_key: Pubkey,

    /// Authority to determine the debt due for distributions.
    pub debt_accountant_key: Pubkey,

    /// Authority to determine the rewards for contributors.
    pub rewards_accountant_key: Pubkey,

    /// Authority to establish new contributor rewards accounts.
    pub contributor_manager_key: Pubkey,

    /// Authority to allow access to the DoubleZero Ledger network.
    pub dz_ledger_sentinel_key: Pubkey,

    /// The program allowed to CPI to this program to withdraw SOL to swap for
    /// 2Z. The Revenue Distribution program will be verifying that the SOL/2Z
    /// Swap program will be transferring 2Z when it withdraws SOL.
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
    pub const FLAG_IS_MIGRATED_BIT: usize = 1;

    pub fn find_address() -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX], &crate::ID)
    }

    pub fn checked_reserve_2z_address(&self) -> Option<Pubkey> {
        if self.reserve_2z_bump_seed == 0 {
            return None;
        }

        let key =
            Pubkey::create_program_address(&[Self::SEED_PREFIX, &[self.bump_seed]], &crate::ID)
                .ok()?;
        checked_2z_token_pda_address(&key, self.reserve_2z_bump_seed)
    }

    pub fn checked_swap_authority_address(&self) -> Option<Pubkey> {
        if self.swap_authority_bump_seed == 0 {
            return None;
        }

        Pubkey::create_program_address(
            &[
                super::SWAP_AUTHORITY_SEED_PREFIX,
                &[self.swap_authority_bump_seed],
            ],
            &crate::ID,
        )
        .ok()
    }

    pub fn checked_swap_destination_2z_address(&self) -> Option<Pubkey> {
        let swap_authority_key = self.checked_swap_authority_address()?;
        checked_2z_token_pda_address(&swap_authority_key, self.swap_destination_2z_bump_seed)
    }

    pub fn checked_withdraw_sol_authority_address(&self) -> Option<Pubkey> {
        if self.sol_2z_swap_program_id == Pubkey::default() {
            return None;
        }

        Pubkey::create_program_address(
            &[
                super::WITHDRAW_SOL_AUTHORITY_SEED_PREFIX,
                &[self.withdraw_sol_authority_bump_seed],
            ],
            &self.sol_2z_swap_program_id,
        )
        .ok()
    }

    pub fn is_paused(&self) -> bool {
        self.flags.bit(Self::FLAG_IS_PAUSED_BIT)
    }

    pub fn set_is_paused(&mut self, should_pause: bool) {
        self.flags.set_bit(Self::FLAG_IS_PAUSED_BIT, should_pause);
    }

    pub fn is_migrated(&self) -> bool {
        self.flags.bit(Self::FLAG_IS_MIGRATED_BIT)
    }

    pub fn set_is_migrated(&mut self, should_migrate: bool) {
        self.flags
            .set_bit(Self::FLAG_IS_MIGRATED_BIT, should_migrate);
    }

    pub fn checked_solana_validator_fee_parameters(&self) -> Option<SolanaValidatorFeeParameters> {
        let params = self.distribution_parameters.solana_validator_fee_parameters;

        if params == Default::default() {
            None
        } else {
            Some(params)
        }
    }

    pub fn checked_distribute_rewards_relay_lamports(&self) -> Option<u32> {
        let lamports = self.relay_parameters.distribute_rewards_lamports;

        if lamports == 0 {
            None
        } else {
            Some(lamports)
        }
    }

    pub fn checked_prepaid_connection_termination_relay_lamports(&self) -> Option<u32> {
        let lamports = self
            .relay_parameters
            .prepaid_connection_termination_lamports;

        if lamports == 0 {
            None
        } else {
            Some(lamports)
        }
    }

    pub fn checked_minimum_epoch_duration_to_finalize_rewards(&self) -> Option<EpochDuration> {
        let duration = self
            .distribution_parameters
            .minimum_epoch_duration_to_finalize_rewards;

        if duration == 0 {
            None
        } else {
            Some(duration.into())
        }
    }
}

//

const _: () = assert!(
    size_of::<ProgramConfig>() == 1_096,
    "`ProgramConfig` size changed"
);
