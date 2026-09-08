use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    types::{Flags, StorageGap},
    Discriminator, PrecomputedDiscriminator,
};
use solana_msg::msg;
use solana_program_error::{ProgramError, ProgramResult};
use solana_pubkey::Pubkey;

use super::TierParameters;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ProgramConfig {
    pub flags: Flags,

    /// Set by the program's upgrade authority. Configures the program; cannot move bonds.
    pub admin_key: Pubkey,

    pub bump_seed: u8,

    _padding: [u8; 7],

    /// What a bond costs at each rate tier. Zero until an admin sets it, which is why a fresh
    /// deployment starts paused: it can size no bond.
    pub tier_parameters: TierParameters,

    _storage_gap: StorageGap<3>,
}

impl PrecomputedDiscriminator for ProgramConfig {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::program_config");
}

impl ProgramConfig {
    pub const SEED_PREFIX: &'static [u8] = b"program_config";

    pub const FLAG_IS_PAUSED_BIT: usize = 0;

    pub fn find_address() -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX], &crate::ID)
    }

    pub fn is_paused(&self) -> bool {
        self.flags.bit(Self::FLAG_IS_PAUSED_BIT)
    }

    pub fn set_is_paused(&mut self, paused: bool) {
        self.flags.set_bit(Self::FLAG_IS_PAUSED_BIT, paused);
    }

    /// Reject an instruction while the program is paused. A new deployment starts paused, so an
    /// unconfigured program takes no bonds.
    pub fn try_require_unpaused(&self) -> ProgramResult {
        if self.is_paused() {
            msg!("Program is paused");
            return Err(ProgramError::InvalidAccountData);
        }
        Ok(())
    }

    /// Reject a caller that is not the configured admin. A zeroed admin key matches nobody, so a
    /// program whose admin was never set is closed rather than open.
    pub fn try_require_admin(&self, signer_key: &Pubkey) -> ProgramResult {
        if self.admin_key == Pubkey::default() || &self.admin_key != signer_key {
            msg!("Signer {} is not the admin", signer_key);
            return Err(ProgramError::IncorrectAuthority);
        }
        Ok(())
    }
}
