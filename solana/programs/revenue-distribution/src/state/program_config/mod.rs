mod community_burn_rate;
mod distribution;
mod relay;

pub use community_burn_rate::*;
pub use distribution::*;
pub use relay::*;

//

use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{types::Flags, Discriminator, PrecomputedDiscriminator};
use solana_account_info::MAX_PERMITTED_DATA_INCREASE;
use solana_pubkey::Pubkey;

use crate::types::{DoubleZeroEpoch, EpochDuration};

use super::checked_2z_token_pda_address;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ProgramConfig {
    pub flags: Flags,

    pub next_completed_dz_epoch: DoubleZeroEpoch,

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

    _padding_0: [u8; 3],

    pub admin_key: Pubkey,

    /// Authority to determine the debt due for distributions.
    pub debt_accountant_key: Pubkey,

    /// Authority to determine the rewards for contributors.
    pub rewards_accountant_key: Pubkey,

    /// Authority to establish new contributor rewards accounts.
    pub contributor_manager_key: Pubkey,

    pub _placeholder_key: Pubkey,

    /// The program allowed to CPI to this program to withdraw SOL to swap for
    /// 2Z. The Revenue Distribution program will be verifying that the SOL/2Z
    /// Swap program will be transferring 2Z when it withdraws SOL.
    pub sol_2z_swap_program_id: Pubkey,

    pub distribution_parameters: DistributionParameters,

    pub relay_parameters: RelayParameters,

    pub last_initialized_distribution_timestamp: u32,
    _padding_1: [u8; 4],

    /// DoubleZero epoch when the debt write-off feature activates. For more
    /// information, please refer to [RFC-0002].
    ///
    /// [RFC-0002]: https://github.com/doublezerofoundation/doublezero-solana/blob/main/docs/rfc/0002_IMPROVED_DEBT_WRITE_OFF_TRACKING.md
    pub debt_write_off_feature_activation_epoch: DoubleZeroEpoch,
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

    pub fn checked_calculation_grace_period_seconds(&self) -> Option<u32> {
        let grace_period = self
            .distribution_parameters
            .calculation_grace_period_minutes;

        if grace_period == 0 {
            None
        } else {
            Some(u32::from(grace_period) * 60)
        }
    }

    pub fn checked_distribution_initialization_grace_period_seconds(&self) -> Option<u32> {
        let grace_period = self
            .distribution_parameters
            .initialization_grace_period_minutes;

        if grace_period == 0 {
            None
        } else {
            Some(u32::from(grace_period) * 60)
        }
    }

    pub fn last_completed_epoch(&self) -> Option<DoubleZeroEpoch> {
        self.next_completed_dz_epoch.checked_sub_duration(1)
    }

    pub fn is_debt_write_off_feature_activated(&self) -> bool {
        let activation_epoch = self.debt_write_off_feature_activation_epoch;

        activation_epoch != 0 && self.next_completed_dz_epoch >= activation_epoch
    }
}

//

const _: () = assert!(
    size_of::<ProgramConfig>() == 600,
    "`ProgramConfig` size changed"
);

const _: () = assert!(
    doublezero_program_tools::zero_copy::data_end::<ProgramConfig>() <= MAX_PERMITTED_DATA_INCREASE,
    "`ProgramConfig` total data length exceeds 10kb"
);

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_is_paused() {
        let mut program_config = ProgramConfig::default();
        assert!(!program_config.is_paused());

        program_config.set_is_paused(true);
        assert!(program_config.is_paused());

        program_config.set_is_paused(false);
        assert!(!program_config.is_paused());
    }

    #[test]
    fn test_is_migrated() {
        let mut program_config = ProgramConfig::default();
        assert!(!program_config.is_migrated());

        program_config.set_is_migrated(true);
        assert!(program_config.is_migrated());

        program_config.set_is_migrated(false);
        assert!(!program_config.is_migrated());
    }

    #[test]
    fn test_checked_solana_validator_fee_parameters() {
        const FIXED_SOL_AMOUNT: u32 = 69;

        let mut program_config = ProgramConfig::default();
        assert!(program_config
            .checked_solana_validator_fee_parameters()
            .is_none());

        program_config
            .distribution_parameters
            .solana_validator_fee_parameters
            .fixed_sol_amount = FIXED_SOL_AMOUNT;

        let mut expected_params = SolanaValidatorFeeParameters::default();
        expected_params.fixed_sol_amount = FIXED_SOL_AMOUNT;
        assert_eq!(
            program_config
                .checked_solana_validator_fee_parameters()
                .unwrap(),
            expected_params
        );
    }

    #[test]
    fn test_checked_distribute_rewards_relay_lamports() {
        const DISTRIBUTE_REWARDS_RELAY_LAMPORTS: u32 = 69;

        let mut program_config = ProgramConfig::default();
        assert!(program_config
            .checked_distribute_rewards_relay_lamports()
            .is_none());

        program_config.relay_parameters.distribute_rewards_lamports =
            DISTRIBUTE_REWARDS_RELAY_LAMPORTS;
        assert_eq!(
            program_config
                .checked_distribute_rewards_relay_lamports()
                .unwrap(),
            DISTRIBUTE_REWARDS_RELAY_LAMPORTS
        );
    }

    #[test]
    fn test_checked_minimum_epoch_duration_to_finalize_rewards() {
        const MINIMUM_EPOCH_DURATION_TO_FINALIZE_REWARDS: u8 = 69;

        let mut program_config = ProgramConfig::default();
        assert!(program_config
            .checked_minimum_epoch_duration_to_finalize_rewards()
            .is_none());

        program_config
            .distribution_parameters
            .minimum_epoch_duration_to_finalize_rewards =
            MINIMUM_EPOCH_DURATION_TO_FINALIZE_REWARDS;
        assert_eq!(
            program_config
                .checked_minimum_epoch_duration_to_finalize_rewards()
                .unwrap(),
            MINIMUM_EPOCH_DURATION_TO_FINALIZE_REWARDS.into()
        );
    }

    #[test]
    fn test_checked_calculation_grace_period_seconds() {
        const CALCULATION_GRACE_PERIOD_SECONDS: u16 = 69;

        let mut program_config = ProgramConfig::default();
        assert!(program_config
            .checked_calculation_grace_period_seconds()
            .is_none());

        program_config
            .distribution_parameters
            .calculation_grace_period_minutes = CALCULATION_GRACE_PERIOD_SECONDS;
        assert_eq!(
            program_config
                .checked_calculation_grace_period_seconds()
                .unwrap(),
            u32::from(CALCULATION_GRACE_PERIOD_SECONDS) * 60
        );
    }

    #[test]
    fn test_checked_distribution_initialization_grace_period_seconds() {
        const DISTRIBUTION_INITIALIZATION_GRACE_PERIOD_SECONDS: u16 = 69;

        let mut program_config = ProgramConfig::default();
        assert!(program_config
            .checked_distribution_initialization_grace_period_seconds()
            .is_none());

        program_config
            .distribution_parameters
            .initialization_grace_period_minutes = DISTRIBUTION_INITIALIZATION_GRACE_PERIOD_SECONDS;
        assert_eq!(
            program_config
                .checked_distribution_initialization_grace_period_seconds()
                .unwrap(),
            u32::from(DISTRIBUTION_INITIALIZATION_GRACE_PERIOD_SECONDS) * 60
        );
    }

    #[test]
    fn test_last_completed_epoch() {
        let mut program_config = ProgramConfig::default();
        assert!(program_config.last_completed_epoch().is_none());

        program_config.next_completed_dz_epoch = program_config
            .next_completed_dz_epoch
            .saturating_add_duration(1);
        assert_eq!(
            program_config.last_completed_epoch().unwrap(),
            DoubleZeroEpoch::new(0)
        );

        program_config.next_completed_dz_epoch = program_config
            .next_completed_dz_epoch
            .saturating_add_duration(1);
        assert_eq!(
            program_config.last_completed_epoch().unwrap(),
            DoubleZeroEpoch::new(1)
        );
    }

    #[test]
    fn test_is_debt_write_off_feature_activated() {
        let mut program_config = ProgramConfig {
            next_completed_dz_epoch: DoubleZeroEpoch::new(1),
            ..Default::default()
        };
        assert!(!program_config.is_debt_write_off_feature_activated());

        program_config.debt_write_off_feature_activation_epoch = DoubleZeroEpoch::new(2);
        assert!(!program_config.is_debt_write_off_feature_activated());

        program_config.next_completed_dz_epoch = program_config
            .next_completed_dz_epoch
            .saturating_add_duration(1);
        assert!(program_config.is_debt_write_off_feature_activated());

        program_config.next_completed_dz_epoch = program_config
            .next_completed_dz_epoch
            .saturating_add_duration(1);
        assert!(program_config.is_debt_write_off_feature_activated());
    }
}
