use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    resource::{IdOrIp, ResourceType},
    serializer::{try_acc_close, try_acc_write},
    state::{
        device::Device, globalstate::GlobalState, resource_extension::ResourceExtensionBorrowed,
        tenant::Tenant, user::*,
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
pub struct UserCloseAccountArgs {
    /// Number of DzPrefixBlock accounts passed for on-chain deallocation.
    /// When 0, legacy behavior is used (no deallocation). When > 0, on-chain deallocation is used.
    #[incremental(default = 0)]
    pub dz_prefix_count: u8,
    /// Whether MulticastPublisherBlock account is passed (1 = yes, 0 = no).
    #[incremental(default = 0)]
    pub multicast_publisher_count: u8,
}

impl fmt::Debug for UserCloseAccountArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "dz_prefix_count: {}, multicast_publisher_count: {}",
            self.dz_prefix_count, self.multicast_publisher_count
        )
    }
}

pub fn process_closeaccount_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserCloseAccountArgs,
) -> ProgramResult {
    #[cfg(test)]
    msg!("process_closeaccount_user({:?})", value);

    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension accounts for on-chain deallocation (before payer)
    // Account layout WITH ResourceExtension (dz_prefix_count > 0):
    //   [user, owner, device, globalstate, global_resource_ext, multicast_publisher_block_ext?, device_tunnel_ids_ext, dz_prefix_ext_0..N, payer, system]
    // Account layout WITHOUT (legacy, dz_prefix_count == 0):
    //   [user, owner, device, globalstate, payer, system]
    let resource_extension_accounts = if value.dz_prefix_count > 0 {
        let global_resource_ext = next_account_info(accounts_iter)?; // UserTunnelBlock

        // Optional MulticastPublisherBlock account
        let multicast_publisher_block_ext = if value.multicast_publisher_count > 0 {
            Some(next_account_info(accounts_iter)?)
        } else {
            None
        };

        let device_tunnel_ids_ext = next_account_info(accounts_iter)?; // TunnelIds

        // Collect DzPrefixBlock accounts based on dz_prefix_count from args
        let mut dz_prefix_accounts = Vec::with_capacity(value.dz_prefix_count as usize);
        for _ in 0..value.dz_prefix_count {
            dz_prefix_accounts.push(next_account_info(accounts_iter)?);
        }

        Some((
            global_resource_ext,
            multicast_publisher_block_ext,
            device_tunnel_ids_ext,
            dz_prefix_accounts,
        ))
    } else {
        None
    };

    // Check if tenant account is provided (if user has tenant assigned)
    // We need to peek at the user to know if tenant is assigned, but we'll validate later
    // For now, we'll check account count to determine if tenant account is provided
    // Account layouts:
    // WITHOUT tenant: [user, owner, device, globalstate, [optional resource extensions], payer, system]
    // WITH tenant: [user, owner, device, globalstate, [optional resource extensions], tenant, payer, system]

    // We'll read the user first to check if it has a tenant
    let user_peek = User::try_from(user_account)?;
    let has_tenant = user_peek.tenant_pk != Pubkey::default();

    let tenant_account = if has_tenant {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

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

    // Deallocate resources from ResourceExtension if accounts provided
    // Deallocation is idempotent - safe to call even if resources weren't allocated
    if let Some((
        global_resource_ext,
        multicast_publisher_block_ext,
        device_tunnel_ids_ext,
        dz_prefix_accounts,
    )) = resource_extension_accounts
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

        // Validate multicast_publisher_block_ext (MulticastPublisherBlock) if provided
        if let Some(multicast_publisher_ext) = multicast_publisher_block_ext {
            assert_eq!(
                multicast_publisher_ext.owner, program_id,
                "Invalid ResourceExtension Account Owner for MulticastPublisherBlock"
            );
            assert!(
                multicast_publisher_ext.is_writable,
                "ResourceExtension Account for MulticastPublisherBlock is not writable"
            );
            assert!(
                !multicast_publisher_ext.data_is_empty(),
                "ResourceExtension Account for MulticastPublisherBlock is empty"
            );

            let (expected_multicast_publisher_pda, _, _) =
                get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
            assert_eq!(
                multicast_publisher_ext.key, &expected_multicast_publisher_pda,
                "Invalid ResourceExtension PDA for MulticastPublisherBlock"
            );
        }

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

        // Deallocate dz_ip (try MulticastPublisherBlock first, then DzPrefixBlock)
        // Only deallocate if dz_ip is allocated (not client_ip and not UNSPECIFIED)
        if user.dz_ip != user.client_ip && user.dz_ip != Ipv4Addr::UNSPECIFIED {
            if let Ok(dz_ip_net) = NetworkV4::new(user.dz_ip, 32) {
                let mut deallocated = false;

                // Try MulticastPublisherBlock first (for publishers)
                if let Some(multicast_publisher_ext) = multicast_publisher_block_ext {
                    let mut buffer = multicast_publisher_ext.data.borrow_mut();
                    let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
                    deallocated = resource.deallocate(&IdOrIp::Ip(dz_ip_net));
                    #[cfg(test)]
                    msg!(
                        "Deallocated dz_ip {} from MulticastPublisherBlock: {}",
                        dz_ip_net,
                        deallocated
                    );
                }

                // Fall back to DzPrefixBlock if not in MulticastPublisherBlock
                if !deallocated {
                    for dz_prefix_account in dz_prefix_accounts.iter() {
                        let mut buffer = dz_prefix_account.data.borrow_mut();
                        let mut resource =
                            ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
                        deallocated = resource.deallocate(&IdOrIp::Ip(dz_ip_net));
                        #[cfg(test)]
                        msg!(
                            "Deallocated dz_ip {} from DzPrefixBlock {:?}: {}",
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
    }

    // Decrement tenant reference count if user has tenant assigned
    if let Some(tenant_acc) = tenant_account {
        // Validate tenant account
        assert_eq!(
            tenant_acc.key, &user.tenant_pk,
            "Tenant account doesn't match user's tenant"
        );
        assert_eq!(tenant_acc.owner, program_id, "Invalid Tenant Account Owner");
        assert!(tenant_acc.is_writable, "Tenant Account is not writable");

        let mut tenant = Tenant::try_from(tenant_acc)?;
        tenant.reference_count = tenant
            .reference_count
            .checked_sub(1)
            .ok_or(DoubleZeroError::InvalidIndex)?;

        try_acc_write(&tenant, tenant_acc, payer_account, accounts)?;

        #[cfg(test)]
        msg!(
            "Decremented tenant reference_count: {}",
            tenant.reference_count
        );
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
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
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

        let result = process_closeaccount_user(
            &program_id,
            &accounts,
            &UserCloseAccountArgs {
                dz_prefix_count: 0, // legacy path - no ResourceExtension accounts
                multicast_publisher_count: 0,
            },
        );

        assert!(result.is_err());
        let err = result.err().unwrap();
        match err {
            ProgramError::Custom(code) => {
                assert_eq!(code, 36);
            }
            _ => panic!("Unexpected error type: {:?}", err),
        };
    }
}
