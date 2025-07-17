use bytemuck::{Pod, Zeroable};
use solana_pubkey::Pubkey;

use crate::types::DoubleZeroEpoch;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C)]
pub struct PrepaidUser {
    user: Pubkey,
    valid_through_dz_epoch: DoubleZeroEpoch,

    disconnect_beneficiary: Pubkey,
    relay_disconnect_fee: u64,
}
