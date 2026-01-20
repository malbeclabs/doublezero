use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    resource::{IdOrIp, ResourceType},
    serializer::{try_acc_close, try_acc_write},
    state::{
        device::Device, globalstate::GlobalState, resource_extension::ResourceExtensionBorrowed,
        user::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::types::NetworkV4;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct UserCloseAccountArgs {}

impl fmt::Debug for UserCloseAccountArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_closeaccount_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &UserCloseAccountArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension accounts for on-chain deallocation (before payer)
    // Account layout WITH ResourceExtension (8+ accounts):
    //   [user, owner, device, globalstate, global_resource_ext, device_tunnel_ids_ext, dz_prefix_ext_0..N, payer, system]
    //   Minimum 8 accounts (6 base + 2 resource accounts with at least 1 DzPrefixBlock)
    // Account layout WITHOUT (legacy, 6 accounts):
    //   [user, owner, device, globalstate, payer, system]
    let resource_extension_accounts = if accounts.len() >= 8 {
        let global_resource_ext = next_account_info(accounts_iter)?; // UserTunnelBlock
        let device_tunnel_ids_ext = next_account_info(accounts_iter)?; // TunnelIds

        // Collect all remaining DzPrefixBlock accounts (N = accounts.len() - 8)
        // accounts.len() - 6 (base) - 2 (payer, system) = number of resource accounts
        // resource accounts - 2 (global, tunnel_ids) = number of DzPrefixBlock accounts
        let dz_prefix_count = accounts.len() - 8;
        let mut dz_prefix_accounts = Vec::with_capacity(dz_prefix_count);
        for _ in 0..dz_prefix_count {
            dz_prefix_accounts.push(next_account_info(accounts_iter)?);
        }

        Some((
            global_resource_ext,
            device_tunnel_ids_ext,
            dz_prefix_accounts,
        ))
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_user({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        device_account.owner, program_id,
        "Invalid Device Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(user_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;

    // Authorization: allow activator_authority_pk OR foundation_allowlist (matching ActivateUser)
    let is_activator = globalstate.activator_authority_pk == *payer_account.key;
    let is_foundation = globalstate.foundation_allowlist.contains(payer_account.key);
    if !is_activator && !is_foundation {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let user = User::try_from(user_account)?;

    if user.device_pk != *device_account.key {
        return Err(ProgramError::InvalidAccountData);
    }

    if user.owner != *owner_account.key {
        return Err(ProgramError::InvalidAccountData);
    }
    if user.status != UserStatus::Deleting {
        msg!("{:?}", user);
        return Err(solana_program::program_error::ProgramError::Custom(1));
    }

    if !user.publishers.is_empty() || !user.subscribers.is_empty() {
        msg!("{:?}", user);
        return Err(DoubleZeroError::ReferenceCountNotZero.into());
    }

    // Deallocate resources from ResourceExtension if accounts provided
    // Deallocation is idempotent - safe to call even if resources weren't allocated
    if let Some((global_resource_ext, device_tunnel_ids_ext, dz_prefix_accounts)) =
        resource_extension_accounts
    {
        // Validate global_resource_ext (UserTunnelBlock)
        assert_eq!(
            global_resource_ext.owner, program_id,
            "Invalid ResourceExtension Account Owner"
        );
        assert!(
            global_resource_ext.is_writable,
            "ResourceExtension Account is not writable"
        );
        assert!(
            !global_resource_ext.data_is_empty(),
            "ResourceExtension Account is empty"
        );

        let (expected_user_tunnel_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
        assert_eq!(
            global_resource_ext.key, &expected_user_tunnel_pda,
            "Invalid ResourceExtension PDA for UserTunnelBlock"
        );

        // Validate device_tunnel_ids_ext (TunnelIds)
        assert_eq!(
            device_tunnel_ids_ext.owner, program_id,
            "Invalid ResourceExtension Account Owner for TunnelIds"
        );
        assert!(
            device_tunnel_ids_ext.is_writable,
            "ResourceExtension Account for TunnelIds is not writable"
        );
        assert!(
            !device_tunnel_ids_ext.data_is_empty(),
            "ResourceExtension Account for TunnelIds is empty"
        );

        let (expected_tunnel_ids_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::TunnelIds(user.device_pk, 0));
        assert_eq!(
            device_tunnel_ids_ext.key, &expected_tunnel_ids_pda,
            "Invalid ResourceExtension PDA for TunnelIds"
        );

        // Validate all DzPrefixBlock accounts
        for (idx, dz_prefix_account) in dz_prefix_accounts.iter().enumerate() {
            assert_eq!(
                dz_prefix_account.owner, program_id,
                "Invalid ResourceExtension Account Owner for DzPrefixBlock[{}]",
                idx
            );
            assert!(
                dz_prefix_account.is_writable,
                "ResourceExtension Account for DzPrefixBlock[{}] is not writable",
                idx
            );
            assert!(
                !dz_prefix_account.data_is_empty(),
                "ResourceExtension Account for DzPrefixBlock[{}] is empty",
                idx
            );

            let (expected_dz_prefix_pda, _, _) = get_resource_extension_pda(
                program_id,
                ResourceType::DzPrefixBlock(user.device_pk, idx),
            );
            assert_eq!(
                dz_prefix_account.key, &expected_dz_prefix_pda,
                "Invalid ResourceExtension PDA for DzPrefixBlock[{}]",
                idx
            );
        }

        // Deallocate tunnel_net from global UserTunnelBlock
        {
            let mut buffer = global_resource_ext.data.borrow_mut();
            let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
            // Deallocate returns false if not allocated; we proceed regardless (idempotent)
            let _deallocated = resource.deallocate(&IdOrIp::Ip(user.tunnel_net));
            #[cfg(test)]
            msg!(
                "Deallocated tunnel_net {}: {}",
                user.tunnel_net,
                _deallocated
            );
        }

        // Deallocate tunnel_id from device TunnelIds
        {
            let mut buffer = device_tunnel_ids_ext.data.borrow_mut();
            let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
            let _deallocated = resource.deallocate(&IdOrIp::Id(user.tunnel_id));
            #[cfg(test)]
            msg!("Deallocated tunnel_id {}: {}", user.tunnel_id, _deallocated);
        }

        // Deallocate dz_ip from device DzPrefixBlock (only if allocated, not client_ip)
        // Try to deallocate from each DzPrefixBlock until one succeeds
        // dz_ip is allocated when != client_ip and is a valid global unicast address
        if user.dz_ip != user.client_ip && user.dz_ip != Ipv4Addr::UNSPECIFIED {
            if let Ok(dz_ip_net) = NetworkV4::new(user.dz_ip, 32) {
                for dz_prefix_account in dz_prefix_accounts.iter() {
                    let mut buffer = dz_prefix_account.data.borrow_mut();
                    let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
                    let deallocated = resource.deallocate(&IdOrIp::Ip(dz_ip_net));
                    #[cfg(test)]
                    msg!(
                        "Deallocated dz_ip {} from {:?}: {}",
                        dz_ip_net,
                        dz_prefix_account.key,
                        deallocated
                    );
                    if deallocated {
                        break; // Successfully deallocated
                    }
                }
            }
        }
    }

    let mut device = Device::try_from(device_account)?;

    device.reference_count = device.reference_count.saturating_sub(1);
    device.users_count = device.users_count.saturating_sub(1);

    try_acc_write(&device, device_account, payer_account, accounts)?;
    try_acc_close(user_account, owner_account)?;

    #[cfg(test)]
    msg!("CloseAccount: User closed");

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_program::{account_info::AccountInfo, clock::Epoch};

    #[test]
    fn test_closeaccount_user_fails_when_publishers_or_subscribers_not_empty() {
        let program_id = Pubkey::new_unique();

        let user_pk = Pubkey::new_unique();
        let owner_pk = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let globalstate_pk = Pubkey::new_unique();
        let payer_pk = Pubkey::new_unique();

        let mut user_lamports = 0u64;
        let mut device_lamports = 0u64;
        let mut globalstate_lamports = 0u64;
        let mut payer_lamports = 0u64;

        let mut user_data = vec![0u8; 1024];
        let mut device_data = vec![0u8; 512];
        let mut globalstate_data = vec![0u8; 512];

        let user = User {
            account_type: crate::state::accounttype::AccountType::User,
            owner: owner_pk,
            index: 0,
            bump_seed: 0,
            user_type: UserType::IBRL,
            tenant_pk: Pubkey::default(),
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [0, 0, 0, 0].into(),
            dz_ip: [0, 0, 0, 0].into(),
            tunnel_id: 0,
            tunnel_net: doublezero_program_common::types::NetworkV4::default(),
            status: UserStatus::Deleting,
            publishers: vec![Pubkey::new_unique()],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

        let device = Device {
            owner: payer_pk,
            reference_count: 1,
            users_count: 1,
            max_users: 10,
            status: crate::state::device::DeviceStatus::Activated,
            desired_status: crate::state::device::DeviceDesiredStatus::Activated,
            ..Device::default()
        };

        let globalstate = GlobalState {
            activator_authority_pk: payer_pk,
            ..GlobalState::default()
        };

        let user_len = borsh::to_vec(&user).unwrap().len();
        user_data[..user_len].copy_from_slice(&borsh::to_vec(&user).unwrap());

        let device_len = borsh::to_vec(&device).unwrap().len();
        device_data[..device_len].copy_from_slice(&borsh::to_vec(&device).unwrap());

        let globalstate_len = borsh::to_vec(&globalstate).unwrap().len();
        globalstate_data[..globalstate_len].copy_from_slice(&borsh::to_vec(&globalstate).unwrap());

        let user_account = AccountInfo::new(
            &user_pk,
            false,
            true,
            &mut user_lamports,
            &mut user_data,
            &program_id,
            false,
            Epoch::default(),
        );
        let mut owner_lamports = 0u64;
        let owner_account = AccountInfo::new(
            &owner_pk,
            false,
            false,
            &mut owner_lamports,
            &mut [],
            &program_id,
            false,
            Epoch::default(),
        );
        let device_account = AccountInfo::new(
            &device_pk,
            false,
            true,
            &mut device_lamports,
            &mut device_data,
            &program_id,
            false,
            Epoch::default(),
        );
        let globalstate_account = AccountInfo::new(
            &globalstate_pk,
            false,
            false,
            &mut globalstate_lamports,
            &mut globalstate_data,
            &program_id,
            false,
            Epoch::default(),
        );
        let payer_account = AccountInfo::new(
            &payer_pk,
            true,
            true,
            &mut payer_lamports,
            &mut [],
            &program_id,
            false,
            Epoch::default(),
        );
        let system_program_id = solana_program::system_program::id();
        let mut system_program_lamports = 0u64;
        let system_program_account = AccountInfo::new(
            &system_program_id,
            false,
            false,
            &mut system_program_lamports,
            &mut [],
            &system_program_id,
            false,
            Epoch::default(),
        );

        let accounts = vec![
            user_account,
            owner_account,
            device_account,
            globalstate_account,
            payer_account,
            system_program_account,
        ];

        let result = process_closeaccount_user(&program_id, &accounts, &UserCloseAccountArgs {});

        assert!(result.is_err());
        let err = result.err().unwrap();
        match err {
            ProgramError::Custom(code) => {
                assert_eq!(code, 13);
            }
            _ => panic!("Unexpected error type: {:?}", err),
        };
    }
}
