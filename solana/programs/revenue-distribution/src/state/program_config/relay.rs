use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::types::StorageGap;

/// Specific amounts to pay actors that execute instructions on behalf of
/// others.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct RelayParameters {
    pub prepaid_connection_termination_lamports: u32,
    pub distribute_rewards_lamports: u32,

    _storage_gap: StorageGap<1>,
}

impl RelayParameters {
    /// The base transaction cost per signature is 5,000 lamports, so we set the
    /// minimum to one more than that.
    pub const MIN_LAMPORTS: u32 = 5_001;
}
