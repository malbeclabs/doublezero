use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, StorageGap},
    Discriminator, PrecomputedDiscriminator,
};
use doublezero_revenue_distribution::types::DoubleZeroEpoch;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct MockIntegrationDistribution {
    pub dz_epoch: DoubleZeroEpoch,
    pub bump_seed: u8,
    _padding: [u8; 7],

    // Reserved for future flags.
    _flags: Flags,

    _storage_gap: StorageGap<4>,
}

impl PrecomputedDiscriminator for MockIntegrationDistribution {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"mock::account::integration_distribution");
}
