use crate::{
    instructions::GeolocationInstruction,
    processors::{
        geo_probe::{
            add_parent_device::process_add_parent_device, create::process_create_geo_probe,
            delete::process_delete_geo_probe, remove_parent_device::process_remove_parent_device,
            update::process_update_geo_probe,
        },
        geolocation_user::{
            add_target::process_add_target, create::process_create_geolocation_user,
            delete::process_delete_geolocation_user, remove_target::process_remove_target,
            update::process_update_geolocation_user,
            update_payment_status::process_update_payment_status,
        },
        program_config::init::process_init_program_config,
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
        GeolocationInstruction::CreateGeolocationUser(args) => {
            process_create_geolocation_user(program_id, accounts, &args)?
        }
        GeolocationInstruction::UpdateGeolocationUser(args) => {
            process_update_geolocation_user(program_id, accounts, &args)?
        }
        GeolocationInstruction::DeleteGeolocationUser => {
            process_delete_geolocation_user(program_id, accounts)?
        }
        GeolocationInstruction::AddTarget(args) => process_add_target(program_id, accounts, &args)?,
        GeolocationInstruction::RemoveTarget(args) => {
            process_remove_target(program_id, accounts, &args)?
        }
        GeolocationInstruction::UpdatePaymentStatus(args) => {
            process_update_payment_status(program_id, accounts, &args)?
        }
    };

    Ok(())
}
