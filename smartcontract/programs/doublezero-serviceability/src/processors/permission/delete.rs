use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct PermissionDeleteArgs {}

impl fmt::Debug for PermissionDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "PermissionDeleteArgs")
    }
}

pub fn process_delete_permission(
    _program_id: &Pubkey,
    _accounts: &[AccountInfo],
    _value: &PermissionDeleteArgs,
) -> ProgramResult {
    Err(ProgramError::InvalidInstructionData)
}
