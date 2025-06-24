use crate::{
    instructions::TelemetryInstruction,
    processors::telemetry::{
        initialize_dz_samples::process_initialize_dz_latency_samples,
        write_dz_samples::process_write_dz_latency_samples,
    },
};
use solana_program::{
    account_info::AccountInfo, entrypoint, entrypoint::ProgramResult, msg, pubkey::Pubkey,
};

// Program entrypoint
#[cfg(not(feature = "no-entrypoint"))]
entrypoint!(process_instruction);

// Function to route instructions to the correct handler
pub fn process_instruction(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    data: &[u8],
) -> ProgramResult {
    let instruction = TelemetryInstruction::unpack(data)?;

    msg!("Instruction: {:?}", instruction);

    match instruction {
        TelemetryInstruction::InitializeDzLatencySamples(args) => {
            process_initialize_dz_latency_samples(program_id, accounts, &args)?
        }
        TelemetryInstruction::WriteDzLatencySamples(args) => {
            process_write_dz_latency_samples(program_id, accounts, &args)?
        }
    };

    Ok(())
}
