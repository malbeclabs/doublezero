use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, StorageGap},
    Discriminator, PrecomputedDiscriminator,
};
use solana_pubkey::Pubkey;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ProgramConfig {
    pub flags: Flags,

    pub admin_key: Pubkey,

    /// Authority that grants or denies access to the DoubleZero Ledger network.
    pub sentinel_key: Pubkey,

    pub request_deposit_lamports: u64,
    pub request_fee_lamports: u64,

    /// 8 * 32 bytes of a storage gap in case more fields need to be added.
    _storage_gap: StorageGap<8>,
}

impl PrecomputedDiscriminator for ProgramConfig {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::program_config");
}

impl ProgramConfig {
    pub const SEED_PREFIX: &'static [u8] = b"program_config";

    pub const FLAG_IS_PAUSED_BIT: usize = 0;
    pub const FLAG_IS_REQUEST_ACCESS_PAUSED_BIT: usize = 1;

    pub fn find_address() -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX], &crate::ID)
    }

    pub fn is_paused(&self) -> bool {
        self.flags.bit(Self::FLAG_IS_PAUSED_BIT)
    }

    pub fn set_is_paused(&mut self, should_pause: bool) {
        self.flags.set_bit(Self::FLAG_IS_PAUSED_BIT, should_pause);
    }

    pub fn is_request_access_paused(&self) -> bool {
        self.flags.bit(Self::FLAG_IS_REQUEST_ACCESS_PAUSED_BIT)
    }

    pub fn set_is_request_access_paused(&mut self, should_pause: bool) {
        self.flags
            .set_bit(Self::FLAG_IS_REQUEST_ACCESS_PAUSED_BIT, should_pause);
    }

    pub fn checked_request_deposit_lamports(&self) -> Option<u64> {
        let lamports = self.request_deposit_lamports;

        if lamports == 0 {
            None
        } else {
            Some(lamports)
        }
    }
}

const _: () = assert!(
    size_of::<ProgramConfig>() == 344,
    "`ProgramConfig` size changed"
);
