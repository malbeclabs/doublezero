use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::types::StorageGap;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct PrepaidConnectionParameters {
    pub minimum_allowed_dz_epochs: u16,
    pub maximum_entries: u16,
    _padding: [u8; 4],

    pub activation_cost: u32,
    pub cost_per_dz_epoch: u32,

    _storage_gap: StorageGap<8>,
}
