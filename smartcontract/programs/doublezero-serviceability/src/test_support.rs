// Support for testing serviceability program from other crates
use crate::{
    instructions::DoubleZeroInstruction,
    processors::{device::suspend::process_suspend_device, link::suspend::process_suspend_link},
};
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, pubkey::Pubkey};

// NOTE: This duplicates some of the logic from entrypoint.rs but avoids the entrypoint macro
pub fn process_instruction_for_tests(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    data: &[u8],
) -> ProgramResult {
    use crate::processors::{
        device::{activate::process_activate_device, create::process_create_device},
        exchange::create::process_create_exchange,
        globalconfig::set::process_set_globalconfig,
        globalstate::initialize::initialize_global_state,
        link::{activate::process_activate_link, create::process_create_link},
        location::create::process_create_location,
    };

    let instruction = DoubleZeroInstruction::unpack(data)?;

    match instruction {
        DoubleZeroInstruction::InitGlobalState() => initialize_global_state(program_id, accounts)?,
        DoubleZeroInstruction::SetGlobalConfig(value) => {
            process_set_globalconfig(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CreateLocation(value) => {
            process_create_location(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CreateExchange(value) => {
            process_create_exchange(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CreateDevice(value) => {
            process_create_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CreateLink(value) => {
            process_create_link(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ActivateDevice(value) => {
            process_activate_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ActivateLink(value) => {
            process_activate_link(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SuspendDevice(value) => {
            process_suspend_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SuspendLink(value) => {
            process_suspend_link(program_id, accounts, &value)?
        }
        _ => {
            // NOTE: For testing, we only need a subset of instructions
            return Ok(());
        }
    };

    Ok(())
}
