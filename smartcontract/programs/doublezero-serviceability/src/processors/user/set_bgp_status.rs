use crate::state::user::BGPStatus;
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, pubkey::Pubkey};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct SetUserBGPStatusArgs {
    pub bgp_status: BGPStatus,
}

impl fmt::Debug for SetUserBGPStatusArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "bgp_status: {}", self.bgp_status)
    }
}

pub fn process_set_bgp_status_user(
    _program_id: &Pubkey,
    _accounts: &[AccountInfo],
    _value: &SetUserBGPStatusArgs,
) -> ProgramResult {
    Err(solana_program::program_error::ProgramError::InvalidInstructionData)
}
