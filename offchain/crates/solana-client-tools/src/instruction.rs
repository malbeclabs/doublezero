use solana_sdk::instruction::Instruction;

/// Unfortunately, Instruction does not implement Default, so we need to replace
/// it with a new Instruction with default values.
pub fn take_instruction(instruction: &mut Instruction) -> Instruction {
    std::mem::replace(
        instruction,
        Instruction {
            program_id: Default::default(),
            accounts: Default::default(),
            data: Default::default(),
        },
    )
}
