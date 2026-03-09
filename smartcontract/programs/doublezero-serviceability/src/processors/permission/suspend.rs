use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct PermissionSuspendArgs {}

impl fmt::Debug for PermissionSuspendArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "PermissionSuspendArgs")
    }
}

pub fn process_suspend_permission(
    _program_id: &Pubkey,
    _accounts: &[AccountInfo],
    _value: &PermissionSuspendArgs,
) -> ProgramResult {
    Err(ProgramError::InvalidInstructionData)
}
