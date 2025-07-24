use bytemuck::{Pod, Zeroable};

use crate::state::{CommunityBurnRateParameters, StorageGap};
use crate::types::ValidatorFee;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct DistributionParameters {
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
    _calculation_grace_period_padding: [u8; 4],

    pub community_burn_rate_parameters: CommunityBurnRateParameters,

    /// Proportion of Solana validator revenue DoubleZero collects to pay contributors. These fees
    /// are denominated in SOL, so this proportion represents a proportion of SOL rewards.
    pub current_solana_validator_fee: ValidatorFee,
    _current_solana_validator_fee_padding: [u8; 6],

    _storage_gap: StorageGap<8>,
}
