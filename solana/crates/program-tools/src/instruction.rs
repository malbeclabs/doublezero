use borsh::BorshSerialize;
use solana_instruction::{AccountMeta, Instruction};
use solana_pubkey::Pubkey;

pub fn try_build_instruction(
    program_id: &Pubkey,
    accounts: impl Into<Vec<AccountMeta>>,
    data: &impl BorshSerialize,
) -> std::io::Result<Instruction> {
    Ok(Instruction {
        program_id: *program_id,
        accounts: accounts.into(),
        data: borsh::to_vec(data)?,
    })
}
