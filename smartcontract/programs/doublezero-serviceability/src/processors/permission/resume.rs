use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct PermissionResumeArgs {}

impl fmt::Debug for PermissionResumeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "PermissionResumeArgs")
    }
}

pub fn process_resume_permission(
    _program_id: &Pubkey,
    _accounts: &[AccountInfo],
    _value: &PermissionResumeArgs,
) -> ProgramResult {
    Err(ProgramError::InvalidInstructionData)
}
