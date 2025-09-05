use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator};

pub const FILLS_CAPACITY: usize = 8;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct FillsRegistry {
    pub fills_count: u32,
    pub head: u32,

    pub fills: [Fill; FILLS_CAPACITY],
}

impl PrecomputedDiscriminator for FillsRegistry {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"mock::account::fills_registry");
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct Fill {
    pub amount_sol_in: u64,
    pub amount_2z_out: u64,
}
