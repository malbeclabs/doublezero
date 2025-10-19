use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator};

// TODO: Reduce and fix in program.
pub const MAX_FILLS_QUEUE_SIZE: usize = 20_000;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct FillsRegistry {
    pub total_sol_pending: u64,
    pub total_2z_pending: u64,
    pub fills: [Fill; MAX_FILLS_QUEUE_SIZE],
    pub head: u64,
    pub tail: u64,
    pub count: u64,
}

impl Default for FillsRegistry {
    fn default() -> Self {
        Self {
            total_sol_pending: 0,
            total_2z_pending: 0,
            fills: [Fill::default(); MAX_FILLS_QUEUE_SIZE],
            head: 0,
            tail: 0,
            count: 0,
        }
    }
}

impl PrecomputedDiscriminator for FillsRegistry {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"account:FillsRegistry");
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct Fill {
    pub amount_sol_in: u64,
    pub amount_2z_out: u64,
}
