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
            user::{add::process_add_user_allowlist, remove::process_remove_user_allowlist},
        },
        device::{
            activate::process_activate_device, closeaccount::process_closeaccount_device,
            create::process_create_device, delete::process_delete_device,
            reject::process_reject_device, resume::process_resume_device,
            suspend::process_suspend_device, update::process_update_device,
        },
        exchange::{
            create::process_create_exchange, delete::process_delete_exchange,
            resume::process_resume_exchange, suspend::process_suspend_exchange,
            update::process_update_exchange,
        },
        globalconfig::set::process_set_globalconfig,
        globalstate::{close::process_close_account, initialize::initialize_global_state},
        location::{
            create::process_create_location, delete::process_delete_location,
            resume::process_resume_location, suspend::process_suspend_location,
            update::process_update_location,
        },
        multicastgroup::{
            activate::process_activate_multicastgroup,
            allowlist::{
                publisher::{
                    add::process_add_multicastgroup_pub_allowlist,
                    remove::process_remove_multicast_pub_allowlist,
                },
                subscriber::{
                    add::process_add_multicastgroup_sub_allowlist,
                    remove::process_remove_multicast_sub_allowlist,
                },
            },
            create::process_create_multicastgroup,
            deactivate::process_deactivate_multicastgroup,
            delete::process_delete_multicastgroup,
            reactivate::process_reactivate_multicastgroup,
            reject::process_reject_multicastgroup,
            subscribe::process_subscribe_multicastgroup,
            suspend::process_suspend_multicastgroup,
            update::process_update_multicastgroup,
        },
        tunnel::{
            activate::process_activate_tunnel, closeaccount::process_closeaccount_tunnel,
            create::process_create_tunnel, delete::process_delete_tunnel,
            reject::process_reject_tunnel, resume::process_resume_tunnel,
            suspend::process_suspend_tunnel, update::process_update_tunnel,
        },
        user::{
            activate::process_activate_user, ban::process_ban_user,
            closeaccount::process_closeaccount_user, create::process_create_user,
            delete::process_delete_user, reject::process_reject_user,
            requestban::process_request_ban_user, resume::process_resume_user,
            suspend::process_suspend_user, update::process_update_user,
        },
    },
};
use solana_program::msg;
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
        DoubleZeroInstruction::None() => {}
        DoubleZeroInstruction::InitGlobalState() => initialize_global_state(program_id, accounts)?,
        DoubleZeroInstruction::CloseAccount(value) => {
            process_close_account(program_id, accounts, &value)?
        }
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
        DoubleZeroInstruction::ResumeLocation(value) => {
            process_resume_location(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ResumeExchange(value) => {
            process_resume_exchange(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ResumeDevice(value) => {
            process_resume_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ResumeTunnel(value) => {
            process_resume_tunnel(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ResumeUser(value) => {
            process_resume_user(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CloseAccountDevice(value) => {
            process_closeaccount_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CloseAccountTunnel(value) => {
            process_closeaccount_tunnel(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CloseAccountUser(value) => {
            process_closeaccount_user(program_id, accounts, &value)?
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

        DoubleZeroInstruction::CreateMulticastGroup(value) => {
            process_create_multicastgroup(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeleteMulticastGroup(value) => {
            process_delete_multicastgroup(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SuspendMulticastGroup(value) => {
            process_suspend_multicastgroup(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ReactivateMulticastGroup(value) => {
            process_reactivate_multicastgroup(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ActivateMulticastGroup(value) => {
            process_activate_multicastgroup(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RejectMulticastGroup(value) => {
            process_reject_multicastgroup(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::UpdateMulticastGroup(value) => {
            process_update_multicastgroup(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeactivateMulticastGroup(value) => {
            process_deactivate_multicastgroup(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(value) => {
            process_add_multicastgroup_pub_allowlist(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(value) => {
            process_remove_multicast_pub_allowlist(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(value) => {
            process_add_multicastgroup_sub_allowlist(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RemoveMulticastGroupSubAllowlist(value) => {
            process_remove_multicast_sub_allowlist(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SubscribeMulticastGroup(value) => {
            process_subscribe_multicastgroup(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CreateSubscribeUser(value) => {
            process_create_subscribe_user(program_id, accounts, &value)?
        }
    };
    Ok(())
}
