use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, pubkey::Pubkey};

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, Clone, PartialEq)]
pub struct TopologyClearArgs {
    pub name: String,
}

pub fn process_topology_clear(
    _program_id: &Pubkey,
    _accounts: &[AccountInfo],
    _value: &TopologyClearArgs,
) -> ProgramResult {
    todo!("TopologyClear not yet implemented")
}
