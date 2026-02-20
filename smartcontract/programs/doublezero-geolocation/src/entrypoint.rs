use crate::{
    instructions::GeolocationInstruction,
    processors::{
        geo_probe::{
            add_parent_device::process_add_parent_device, create::process_create_geo_probe,
            delete::process_delete_geo_probe, remove_parent_device::process_remove_parent_device,
            update::process_update_geo_probe,
        },
        program_config::{
            init::process_init_program_config, update::process_update_program_config,
        },
    },
};

use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, msg, pubkey::Pubkey};

#[cfg(not(feature = "no-entrypoint"))]
solana_program::entrypoint!(process_instruction);

pub fn process_instruction(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    data: &[u8],
) -> ProgramResult {
    let instruction = GeolocationInstruction::unpack(data)?;

    msg!("Instruction: {:?}", instruction);

    match instruction {
        GeolocationInstruction::InitProgramConfig(args) => {
            process_init_program_config(program_id, accounts, &args)?
        }
        GeolocationInstruction::CreateGeoProbe(args) => {
            process_create_geo_probe(program_id, accounts, &args)?
        }
        GeolocationInstruction::UpdateGeoProbe(args) => {
            process_update_geo_probe(program_id, accounts, &args)?
        }
        GeolocationInstruction::DeleteGeoProbe => process_delete_geo_probe(program_id, accounts)?,
        GeolocationInstruction::AddParentDevice(args) => {
            process_add_parent_device(program_id, accounts, &args)?
        }
        GeolocationInstruction::RemoveParentDevice(args) => {
            process_remove_parent_device(program_id, accounts, &args)?
        }
        GeolocationInstruction::UpdateProgramConfig(args) => {
            process_update_program_config(program_id, accounts, &args)?
        }
    };

    Ok(())
}
