use solana_program::msg;
use crate::{
    instructions::*,
    processors::{
        allowlist::{
            device::{
                add::process_add_device_allowlist_globalconfig,
                remove::process_remove_device_allowlist_globalconfig,
            },
            foundation::{
                add::process_add_foundation_allowlist_globalconfig,
                remove::process_remove_foundation_allowlist_globalconfig,
            },
            user::{
                add::process_add_user_allowlist,
                remove::process_remove_user_allowlist,
            },
        },
        device::{
            activate::process_activate_device, create::process_create_device,
            deactivate::process_deactivate_device, delete::process_delete_device,
            reactivate::process_reactivate_device, reject::process_reject_device,
            suspend::process_suspend_device, update::process_update_device,
        },
        exchange::{
            create::process_create_exchange, delete::process_delete_exchange,
            reactivate::process_reactivate_exchange, suspend::process_suspend_exchange,
            update::process_update_exchange,
        },
        globalconfig::set::process_set_globalconfig,
        globalstate::initialize::initialize_global_state,
        location::{
            create::process_create_location, delete::process_delete_location,
            reactivate::process_reactivate_location, suspend::process_suspend_location,
            update::process_update_location,
        },
        tunnel::{
            activate::process_activate_tunnel, create::process_create_tunnel,
            deactivate::process_deactivate_tunnel, delete::process_delete_tunnel,
            reactivate::process_reactivate_tunnel, reject::process_reject_tunnel,
            suspend::process_suspend_tunnel, update::process_update_tunnel,
        },
        user::{
            activate::process_activate_user, ban::process_ban_user, create::process_create_user,
            deactivate::process_deactivate_user, delete::process_delete_user,
            reactivate::process_reactivate_user, reject::process_reject_user,
            requestban::process_request_ban_user, suspend::process_suspend_user,
            update::process_update_user,
        },
    },
};
use solana_program::{
    account_info::AccountInfo, entrypoint, entrypoint::ProgramResult, pubkey::Pubkey,
};

// Program entrypoint
entrypoint!(process_instruction);

// Function to route instructions to the correct handler
pub fn process_instruction(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    data: &[u8],
) -> ProgramResult {
    let instruction = DoubleZeroInstruction::unpack(data)?;

    msg!("Instruction: {:?}", instruction);

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
        DoubleZeroInstruction::UpdateDevice(value) => {
            process_update_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CreateTunnel(value) => {
            process_create_tunnel(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CreateUser(value) => {
            process_create_user(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ActivateTunnel(value) => {
            process_activate_tunnel(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ActivateDevice(value) => {
            process_activate_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ActivateUser(value) => {
            process_activate_user(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeleteUser(value) => {
            process_delete_user(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeleteDevice(value) => {
            process_delete_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeleteTunnel(value) => {
            process_delete_tunnel(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeleteExchange(value) => {
            process_delete_exchange(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeleteLocation(value) => {
            process_delete_location(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::UpdateLocation(value) => {
            process_update_location(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::UpdateExchange(value) => {
            process_update_exchange(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::UpdateTunnel(value) => {
            process_update_tunnel(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::UpdateUser(value) => {
            process_update_user(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SuspendLocation(value) => {
            process_suspend_location(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SuspendExchange(value) => {
            process_suspend_exchange(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SuspendDevice(value) => {
            process_suspend_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SuspendTunnel(value) => {
            process_suspend_tunnel(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SuspendUser(value) => {
            process_suspend_user(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ReactivateLocation(value) => {
            process_reactivate_location(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ReactivateExchange(value) => {
            process_reactivate_exchange(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ReactivateDevice(value) => {
            process_reactivate_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ReactivateTunnel(value) => {
            process_reactivate_tunnel(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ReactivateUser(value) => {
            process_reactivate_user(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeactivateDevice(value) => {
            process_deactivate_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeactivateTunnel(value) => {
            process_deactivate_tunnel(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeactivateUser(value) => {
            process_deactivate_user(program_id, accounts, &value)?
        }

        DoubleZeroInstruction::RejectDevice(value) => {
            process_reject_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RejectTunnel(value) => {
            process_reject_tunnel(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RejectUser(value) => {
            process_reject_user(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::AddFoundationAllowlist(value) => {
            process_add_foundation_allowlist_globalconfig(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RemoveFoundationAllowlist(value) => {
            process_remove_foundation_allowlist_globalconfig(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::AddDeviceAllowlist(value) => {
            process_add_device_allowlist_globalconfig(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RemoveDeviceAllowlist(value) => {
            process_remove_device_allowlist_globalconfig(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::AddUserAllowlist(value) => {
            process_add_user_allowlist(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RemoveUserAllowlist(value) => {
            process_remove_user_allowlist(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RequestBanUser(value) => {
            process_request_ban_user(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::BanUser(value) => process_ban_user(program_id, accounts, &value)?,
    };
    Ok(())
}
