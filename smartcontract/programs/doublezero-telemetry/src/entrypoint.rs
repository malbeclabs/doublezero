use crate::{
    instructions::TelemetryInstruction,
    processors::telemetry::{
        initialize_device_latency_samples::process_initialize_device_latency_samples,
        write_device_latency_samples::process_write_device_latency_samples,
    },
};

use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, msg, pubkey::Pubkey};

// Program entrypoint
#[cfg(not(feature = "no-entrypoint"))]
solana_program::entrypoint!(process_instruction);

// Function to route instructions to the correct handler
pub fn process_instruction(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    data: &[u8],
) -> ProgramResult {
    let instruction = TelemetryInstruction::unpack(data)?;

    msg!("Instruction: {:?}", instruction);

    match instruction {
        TelemetryInstruction::InitializeDeviceLatencySamples(args) => {
            process_initialize_device_latency_samples(program_id, accounts, &args)?
        }
        TelemetryInstruction::WriteDeviceLatencySamples(args) => {
            process_write_device_latency_samples(program_id, accounts, &args)?
        }
    };

    Ok(())
}
