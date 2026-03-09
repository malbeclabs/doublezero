use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct PermissionCreateArgs {
    /// The pubkey for which this Permission PDA is being created.
    pub user_payer: Pubkey,
    /// Bitmask of permission_flags to grant.
    pub permissions: u128,
}

impl fmt::Debug for PermissionCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_payer: {}, permissions: {}",
            self.user_payer, self.permissions
        )
    }
}

pub fn process_create_permission(
    _program_id: &Pubkey,
    _accounts: &[AccountInfo],
    _value: &PermissionCreateArgs,
) -> ProgramResult {
    Err(ProgramError::InvalidInstructionData)
}
