use bytemuck::{Pod, Zeroable};

use crate::state::StorageGap;

/// Specific amounts to pay actors that execute instructions on behalf of others.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct RelayParameters {
    pub prepaid_connection_termination_lamports: u32,
    pub contributor_reward_claim_lamports: u32,

    _storage_gap: StorageGap<1>,
}
