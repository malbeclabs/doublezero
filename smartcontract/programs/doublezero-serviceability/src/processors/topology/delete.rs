use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, pubkey::Pubkey};

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, Clone, PartialEq)]
pub struct TopologyDeleteArgs {
    pub name: String,
}

pub fn process_topology_delete(
    _program_id: &Pubkey,
    _accounts: &[AccountInfo],
    _value: &TopologyDeleteArgs,
) -> ProgramResult {
    todo!("TopologyDelete not yet implemented")
}
