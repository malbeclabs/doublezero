use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct PermissionUpdateArgs {
    /// New permissions bitmask to replace the existing one.
    pub permissions: u128,
}

impl fmt::Debug for PermissionUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "permissions: {}", self.permissions)
    }
}

pub fn process_update_permission(
    _program_id: &Pubkey,
    _accounts: &[AccountInfo],
    _value: &PermissionUpdateArgs,
) -> ProgramResult {
    Err(ProgramError::InvalidInstructionData)
}
