use crate::{
    error::DoubleZeroError,
    instructions::*,
    processors::{
        accesspass::{
            check_status::process_check_status_access_pass, close::process_close_access_pass,
            set::process_set_access_pass,
        },
        allowlist::{
            foundation::{
                add::process_add_foundation_allowlist_globalconfig,
                remove::process_remove_foundation_allowlist_globalconfig,
            },
            qa::{
                add::process_add_qa_allowlist_globalconfig,
                remove::process_remove_qa_allowlist_globalconfig,
            },
        },
        contributor::{
            create::process_create_contributor, delete::process_delete_contributor,
            resume::process_resume_contributor, suspend::process_suspend_contributor,
            update::process_update_contributor,
        },
        device::{
            activate::process_activate_device,
            closeaccount::process_closeaccount_device,
            create::process_create_device,
            delete::process_delete_device,
            interface::{
                activate::process_activate_device_interface,
                create::process_create_device_interface, delete::process_delete_device_interface,
                reject::process_reject_device_interface, remove::process_remove_device_interface,
                unlink::process_unlink_device_interface, update::process_update_device_interface,
            },
            reject::process_reject_device,
            sethealth::process_set_health_device,
            update::process_update_device,
        },
        exchange::{
            create::process_create_exchange, delete::process_delete_exchange,
            resume::process_resume_exchange, setdevice::process_setdevice_exchange,
            suspend::process_suspend_exchange, update::process_update_exchange,
        },
        globalconfig::set::process_set_globalconfig,
        globalstate::{
            initialize::initialize_global_state, setairdrop::process_set_airdrop,
            setauthority::process_set_authority, setfeatureflags::process_set_feature_flags,
            setversion::process_set_version,
        },
        link::{
            accept::process_accept_link, activate::process_activate_link,
            closeaccount::process_closeaccount_link, create::process_create_link,
            delete::process_delete_link, reject::process_reject_link,
            sethealth::process_set_health_link, update::process_update_link,
        },
        location::{
            create::process_create_location, delete::process_delete_location,
            resume::process_resume_location, suspend::process_suspend_location,
            update::process_update_location,
        },
        migrate::process_migrate,
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
            closeaccount::process_closeaccount_multicastgroup,
            create::process_create_multicastgroup,
            delete::process_delete_multicastgroup,
            reactivate::process_reactivate_multicastgroup,
            reject::process_reject_multicastgroup,
            subscribe::process_subscribe_multicastgroup,
            suspend::process_suspend_multicastgroup,
            update::process_update_multicastgroup,
        },
        resource::{
            allocate::process_allocate_resource,
            closeaccount::process_closeaccount_resource_extension, create::process_create_resource,
            deallocate::process_deallocate_resource,
        },
        tenant::{
            add_administrator::process_add_administrator_tenant, create::process_create_tenant,
            delete::process_delete_tenant,
            remove_administrator::process_remove_administrator_tenant,
            update::process_update_tenant, update_payment_status::process_update_payment_status,
        },
        user::{
            activate::process_activate_user, ban::process_ban_user,
            check_access_pass::process_check_access_pass_user,
            closeaccount::process_closeaccount_user, create::process_create_user,
            create_subscribe::process_create_subscribe_user, delete::process_delete_user,
            reject::process_reject_user, requestban::process_request_ban_user,
            update::process_update_user,
        },
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
    let instruction = DoubleZeroInstruction::unpack(data)?;

    msg!("Instruction: {:?}", instruction);

    match instruction {
        DoubleZeroInstruction::Migrate(value) => process_migrate(program_id, accounts, &value)?,
        DoubleZeroInstruction::InitGlobalState() => initialize_global_state(program_id, accounts)?,
        DoubleZeroInstruction::SetAirdrop(value) => {
            process_set_airdrop(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SetAuthority(value) => {
            process_set_authority(program_id, accounts, &value)?
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
        DoubleZeroInstruction::CreateLink(value) => {
            process_create_link(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CreateUser(value) => {
            process_create_user(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ActivateLink(value) => {
            process_activate_link(program_id, accounts, &value)?
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
        DoubleZeroInstruction::DeleteLink(value) => {
            process_delete_link(program_id, accounts, &value)?
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
        DoubleZeroInstruction::UpdateLink(value) => {
            process_update_link(program_id, accounts, &value)?
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
        DoubleZeroInstruction::SuspendDevice() => return Err(DoubleZeroError::Deprecated.into()),
        DoubleZeroInstruction::SuspendLink() => return Err(DoubleZeroError::Deprecated.into()),
        DoubleZeroInstruction::SuspendUser() => {
            return Err(DoubleZeroError::Deprecated.into());
        }
        DoubleZeroInstruction::ResumeLocation(value) => {
            process_resume_location(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ResumeExchange(value) => {
            process_resume_exchange(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ResumeDevice() => return Err(DoubleZeroError::Deprecated.into()),
        DoubleZeroInstruction::ResumeLink() => return Err(DoubleZeroError::Deprecated.into()),
        DoubleZeroInstruction::ResumeUser() => {
            return Err(DoubleZeroError::Deprecated.into());
        }
        DoubleZeroInstruction::CloseAccountDevice(value) => {
            process_closeaccount_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CloseAccountLink(value) => {
            process_closeaccount_link(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CloseAccountUser(value) => {
            process_closeaccount_user(program_id, accounts, &value)?
        }

        DoubleZeroInstruction::RejectDevice(value) => {
            process_reject_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RejectLink(value) => {
            process_reject_link(program_id, accounts, &value)?
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
        DoubleZeroInstruction::AddDeviceAllowlist() => {
            return Err(DoubleZeroError::Deprecated.into());
        }
        DoubleZeroInstruction::RemoveDeviceAllowlist() => {
            return Err(DoubleZeroError::Deprecated.into());
        }
        DoubleZeroInstruction::AddUserAllowlist() => {
            return Err(DoubleZeroError::Deprecated.into());
        }
        DoubleZeroInstruction::RemoveUserAllowlist() => {
            return Err(DoubleZeroError::Deprecated.into());
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
            process_closeaccount_multicastgroup(program_id, accounts, &value)?
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
        DoubleZeroInstruction::CreateContributor(value) => {
            process_create_contributor(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::UpdateContributor(value) => {
            process_update_contributor(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SuspendContributor(value) => {
            process_suspend_contributor(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ResumeContributor(value) => {
            process_resume_contributor(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeleteContributor(value) => {
            process_delete_contributor(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SetDeviceExchange(value) => {
            process_setdevice_exchange(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::AcceptLink(value) => {
            process_accept_link(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SetAccessPass(value) => {
            process_set_access_pass(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CloseAccessPass(value) => {
            process_close_access_pass(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CheckStatusAccessPass(value) => {
            process_check_status_access_pass(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CheckUserAccessPass(value) => {
            process_check_access_pass_user(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::ActivateDeviceInterface(value) => {
            process_activate_device_interface(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CreateDeviceInterface(value) => {
            process_create_device_interface(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeleteDeviceInterface(value) => {
            process_delete_device_interface(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RemoveDeviceInterface(value) => {
            process_remove_device_interface(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::UpdateDeviceInterface(value) => {
            process_update_device_interface(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::UnlinkDeviceInterface(value) => {
            process_unlink_device_interface(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RejectDeviceInterface(value) => {
            process_reject_device_interface(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SetMinVersion(value) => {
            process_set_version(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::AllocateResource(value) => {
            process_allocate_resource(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CreateResource(value) => {
            process_create_resource(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeallocateResource(value) => {
            process_deallocate_resource(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CloseResource(value) => {
            process_closeaccount_resource_extension(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SetDeviceHealth(value) => {
            process_set_health_device(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SetLinkHealth(value) => {
            process_set_health_link(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::AddQaAllowlist(value) => {
            process_add_qa_allowlist_globalconfig(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::RemoveQaAllowlist(value) => {
            process_remove_qa_allowlist_globalconfig(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::CreateTenant(value) => {
            process_create_tenant(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::UpdateTenant(value) => {
            process_update_tenant(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::DeleteTenant(value) => {
            process_delete_tenant(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::TenantAddAdministrator(value) => {
            process_add_administrator_tenant(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::TenantRemoveAdministrator(value) => {
            process_remove_administrator_tenant(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::UpdatePaymentStatus(value) => {
            process_update_payment_status(program_id, accounts, &value)?
        }
        DoubleZeroInstruction::SetFeatureFlags(value) => {
            process_set_feature_flags(program_id, accounts, &value)?
        }
    };
    Ok(())
}
