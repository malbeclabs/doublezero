use crate::{
    error::DoubleZeroError,
    pda::get_accesspass_pda,
    serializer::{try_acc_close, try_acc_write},
    state::{
        accesspass::{AccessPass, AccessPassStatus},
        device::Device,
        globalstate::GlobalState,
        tenant::Tenant,
        user::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

use super::resource_onchain_helpers;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct UserDeleteArgs {
    /// Number of DzPrefixBlock accounts passed for on-chain deallocation.
    /// When 0, legacy behavior (Deleting status). When > 0, atomic delete+deallocate+close.
    #[incremental(default = 0)]
    pub dz_prefix_count: u8,
    /// Whether MulticastPublisherBlock account is passed (1 = yes, 0 = no).
    #[incremental(default = 0)]
    pub multicast_publisher_count: u8,
}

impl fmt::Debug for UserDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "dz_prefix_count: {}, multicast_publisher_count: {}",
            self.dz_prefix_count, self.multicast_publisher_count
        )
    }
}

pub fn process_delete_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: additional accounts for atomic deallocation (between globalstate and payer)
    // Account layout WITH deallocation (dz_prefix_count > 0):
    //   [user, accesspass, globalstate, device, user_tunnel_block, multicast_publisher_block?, device_tunnel_ids, dz_prefix_0..N, optional_tenant, owner, payer, system]
    // Account layout WITHOUT (legacy, dz_prefix_count == 0):
    //   [user, accesspass, globalstate, payer, system]
    let deallocation_accounts = if value.dz_prefix_count > 0 {
        let device_account = next_account_info(accounts_iter)?;
        let user_tunnel_block_ext = next_account_info(accounts_iter)?;

        let multicast_publisher_block_ext = if value.multicast_publisher_count > 0 {
            Some(next_account_info(accounts_iter)?)
        } else {
            None
        };

        let device_tunnel_ids_ext = next_account_info(accounts_iter)?;

        let mut dz_prefix_accounts = Vec::with_capacity(value.dz_prefix_count as usize);
        for _ in 0..value.dz_prefix_count {
            dz_prefix_accounts.push(next_account_info(accounts_iter)?);
        }

        Some((
            device_account,
            user_tunnel_block_ext,
            multicast_publisher_block_ext,
            device_tunnel_ids_ext,
            dz_prefix_accounts,
        ))
    } else {
        None
    };

    // For atomic path: check if user has a tenant (we'll peek at user data)
    // For legacy path: no tenant handling needed
    let tenant_account = if value.dz_prefix_count > 0 {
        // Peek at user to check tenant
        let user_peek = User::try_from(user_account)?;
        if user_peek.tenant_pk != Pubkey::default() {
            Some(next_account_info(accounts_iter)?)
        } else {
            None
        }
    } else {
        None
    };

    // For atomic path, owner account is needed for close
    let owner_account = if value.dz_prefix_count > 0 {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_user({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid AccessPass Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(user_account.is_writable, "PDA Account is not writable");

    let user: User = User::try_from(user_account)?;

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key)
        && user.owner != *payer_account.key
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let (accesspass_pda, _) = get_accesspass_pda(program_id, &user.client_ip, &user.owner);
    let (accesspass_dynamic_pda, _) =
        get_accesspass_pda(program_id, &Ipv4Addr::UNSPECIFIED, &user.owner);
    // Access Pass must exist and match the client_ip or allow_multiple_ip must be enabled
    assert!(
        accesspass_account.key == &accesspass_pda
            || accesspass_account.key == &accesspass_dynamic_pda,
        "Invalid AccessPass PDA",
    );

    if !accesspass_account.data_is_empty() {
        // Read Access Pass
        let mut accesspass = AccessPass::try_from(accesspass_account)?;
        if accesspass.user_payer != user.owner {
            msg!(
                "Invalid user_payer accesspass.user_payer: {} = user_payer: {} ",
                accesspass.user_payer,
                user.owner
            );
            return Err(DoubleZeroError::Unauthorized.into());
        }
        if accesspass.is_dynamic() && accesspass.client_ip == Ipv4Addr::UNSPECIFIED {
            accesspass.client_ip = user.client_ip; // lock to the first used IP
        }
        if accesspass.client_ip != user.client_ip && !accesspass.allow_multiple_ip() {
            msg!(
                "Invalid client_ip accesspass.{{client_ip: {}}} = {{ client_ip: {} }}",
                accesspass.client_ip,
                user.client_ip
            );
            return Err(DoubleZeroError::Unauthorized.into());
        }

        accesspass.connection_count = accesspass.connection_count.saturating_sub(1);
        accesspass.status = if accesspass.connection_count > 0 {
            AccessPassStatus::Connected
        } else {
            AccessPassStatus::Disconnected
        };
        if accesspass.connection_count == 0 && accesspass.allow_multiple_ip() {
            accesspass.client_ip = Ipv4Addr::UNSPECIFIED; // reset to allow multiple IPs
        }

        try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;
    }

    // Status check differs between legacy and atomic paths
    if value.dz_prefix_count > 0 {
        // Atomic: reject Deleting and Updating
        if matches!(user.status, UserStatus::Deleting | UserStatus::Updating) {
            return Err(DoubleZeroError::InvalidStatus.into());
        }
    } else {
        // Legacy: reject Deleting and Updating (same check)
        if matches!(user.status, UserStatus::Deleting | UserStatus::Updating) {
            return Err(DoubleZeroError::InvalidStatus.into());
        }
    }

    if !user.publishers.is_empty() || !user.subscribers.is_empty() {
        msg!("{:?}", user);
        return Err(DoubleZeroError::ReferenceCountNotZero.into());
    }

    if let Some((
        device_account,
        user_tunnel_block_ext,
        multicast_publisher_block_ext,
        device_tunnel_ids_ext,
        dz_prefix_accounts,
    )) = deallocation_accounts
    {
        let owner_account = owner_account.unwrap();

        // Validate additional accounts
        assert_eq!(
            device_account.owner, program_id,
            "Invalid Device Account Owner"
        );

        if user.device_pk != *device_account.key {
            return Err(ProgramError::InvalidAccountData);
        }
        if user.owner != *owner_account.key {
            return Err(ProgramError::InvalidAccountData);
        }

        // Deallocate resources via helper (checks feature flag, validates PDAs)
        resource_onchain_helpers::validate_and_deallocate_user_resources(
            program_id,
            &user,
            user_tunnel_block_ext,
            multicast_publisher_block_ext.as_ref().map(|a| *a),
            device_tunnel_ids_ext,
            &dz_prefix_accounts,
            &globalstate,
        )?;

        // Decrement tenant reference count if user has tenant assigned
        if let Some(tenant_acc) = tenant_account {
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
        }

        // Decrement device counters
        let mut device = Device::try_from(device_account)?;
        device.reference_count = device.reference_count.saturating_sub(1);
        device.users_count = device.users_count.saturating_sub(1);
        match user.user_type {
            UserType::Multicast => {
                device.multicast_users_count = device.multicast_users_count.saturating_sub(1);
            }
            _ => {
                device.unicast_users_count = device.unicast_users_count.saturating_sub(1);
            }
        }

        try_acc_write(&device, device_account, payer_account, accounts)?;
        try_acc_close(user_account, owner_account)?;

        #[cfg(test)]
        msg!("DeleteUser (atomic): User deallocated and closed");
    } else {
        // Legacy path: just mark as Deleting
        let mut user: User = User::try_from(user_account)?;
        user.status = UserStatus::Deleting;

        try_acc_write(&user, user_account, payer_account, accounts)?;

        #[cfg(test)]
        msg!("Deleting: {:?}", user);
    }

    Ok(())
}
