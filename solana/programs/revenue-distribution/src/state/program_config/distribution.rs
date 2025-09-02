use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::types::StorageGap;

use crate::{state::CommunityBurnRateParameters, types::ValidatorFee};

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct DistributionParameters {
    /// Time to wait after the turn of the DZ epoch to perform any calculations.
    ///
    /// This field is not used for anything within the Revenue Distribution
    /// program because there is no way to enforce that calculations are
    /// performed at a specific time. But this field is stored here to act as a
    /// source-of-truth to inform the off-chain process (the accountant) how
    /// long it should wait after the new DZ epoch starts.
    ///
    /// This field also acts as an indication of whether the program config is
    /// initialized. If a grace period has not been configured, the program will
    /// not allow new Merkle roots (which are necessary for validators to pay
    /// their dues and contributors to have rewards distributed).
    pub calculation_grace_period_seconds: u32,

    /// The minimum duration that must pass before rewards can be finalized.
    /// This field is used to ensure that rewards are not finalized (and
    /// distributed) too early.
    pub minimum_epoch_duration_to_finalize_rewards: u16,
    _padding: [u8; 2],

    pub community_burn_rate_parameters: CommunityBurnRateParameters,

    /// Proportion of Solana validator revenue DoubleZero collects to pay
    /// contributors. These fees are denominated in SOL, so this proportion
    /// represents a proportion of SOL rewards.
    pub solana_validator_fee_parameters: SolanaValidatorFeeParameters,

    _storage_gap: StorageGap<8>,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct SolanaValidatorFeeParameters {
    /// Percentage of rewards from base transaction fees.
    pub base_block_rewards_pct: ValidatorFee,

    /// Percentage of rewards from priority transaction fees.
    pub priority_block_rewards_pct: ValidatorFee,

    /// Percentage of rewards from inflation.
    pub inflation_rewards_pct: ValidatorFee,

    /// Percentage of rewards from Jito tips.
    pub jito_tips_pct: ValidatorFee,

    /// Fixed amount of SOL charged to each validator. Maximum configurable
    /// amount is the bound of `u32::MAX`, so about 4.2 SOL.
    pub fixed_sol_amount: u32,

    _storage_gap: [u32; 7],
}
