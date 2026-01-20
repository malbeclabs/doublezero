use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        accesspass::AccessPass,
        globalstate::GlobalState,
        resource_extension::ResourceExtensionBorrowed,
        user::{User, UserStatus, UserType},
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
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct UserActivateArgs {
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub dz_ip: Ipv4Addr,
}

impl fmt::Debug for UserActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "tunnel_id: {}, tunnel_net: {}, dz_ip: {}",
            self.tunnel_id, &self.tunnel_net, &self.dz_ip,
        )
    }
}

pub fn process_activate_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserActivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension accounts for on-chain allocation (before payer)
    // Account layout WITH ResourceExtension (7+ accounts):
    //   [user, accesspass, globalstate, global_resource_ext, device_tunnel_ids_ext, dz_prefix_ext_0..N, payer, system]
    //   Minimum 7 accounts (5 base + 2 resource accounts with at least 1 DzPrefixBlock)
    // Account layout WITHOUT (legacy, 5 accounts):
    //   [user, accesspass, globalstate, payer, system]
    let resource_extension_accounts = if accounts.len() >= 7 {
        let global_resource_ext = next_account_info(accounts_iter)?; // UserTunnelBlock
        let device_tunnel_ids_ext = next_account_info(accounts_iter)?; // TunnelIds

        // Collect all remaining DzPrefixBlock accounts (N = accounts.len() - 7)
        // accounts.len() - 5 (base) - 2 (payer, system) = number of resource accounts
        // resource accounts - 2 (global, tunnel_ids) = number of DzPrefixBlock accounts
        let dz_prefix_count = accounts.len() - 7;
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
    msg!("process_activate_user({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid User Account Owner");
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid AccessPass Account Owner"
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

    // Authorization: allow activator_authority_pk OR foundation_allowlist
    let is_activator = globalstate.activator_authority_pk == *payer_account.key;
    let is_foundation = globalstate.foundation_allowlist.contains(payer_account.key);
    if !is_activator && !is_foundation {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user: User = User::try_from(user_account)?;

    if user.status != UserStatus::Pending && user.status != UserStatus::Updating {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let mut accesspass = AccessPass::try_from(accesspass_account)?;
    if accesspass.user_payer != user.owner {
        msg!(
            "Invalid user_payer accesspass.{{user_payer: {}}} = {{ user_payer: {} }}",
            accesspass.user_payer,
            payer_account.key
        );
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // Allocate resources from ResourceExtension or use provided values
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

        // Allocate tunnel_net from global UserTunnelBlock
        {
            let mut buffer = global_resource_ext.data.borrow_mut();
            let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
            user.tunnel_net = resource
                .allocate()?
                .get_ip()
                .ok_or(DoubleZeroError::InvalidArgument)?;
        }

        // Allocate tunnel_id from device TunnelIds
        {
            let mut buffer = device_tunnel_ids_ext.data.borrow_mut();
            let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
            user.tunnel_id = resource
                .allocate()?
                .get_id()
                .ok_or(DoubleZeroError::InvalidArgument)?;
        }

        // Conditionally allocate dz_ip based on user_type (matching activator behavior)
        let need_dz_ip = match user.user_type {
            UserType::IBRLWithAllocatedIP | UserType::EdgeFiltering => true,
            UserType::IBRL => false,
            UserType::Multicast => !user.publishers.is_empty(),
        };

        if need_dz_ip {
            // Try to allocate from each DzPrefixBlock until one succeeds
            let mut allocated_dz_ip = None;
            for dz_prefix_account in dz_prefix_accounts.iter() {
                let mut buffer = dz_prefix_account.data.borrow_mut();
                let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;

                if let Ok(ip) = resource
                    .allocate()
                    .and_then(|v| v.get_ip().ok_or(DoubleZeroError::InvalidArgument))
                {
                    allocated_dz_ip = Some(ip.ip());
                    break;
                }
            }

            user.dz_ip = allocated_dz_ip.ok_or(DoubleZeroError::AllocationFailed)?;
        } else {
            // Use client_ip, no allocation needed
            user.dz_ip = user.client_ip;
        }
    } else {
        // Legacy behavior: use provided args
        user.tunnel_id = value.tunnel_id;
        user.tunnel_net = value.tunnel_net;
        user.dz_ip = value.dz_ip;
    }

    user.try_activate(&mut accesspass)?;

    try_acc_write(&user, user_account, payer_account, accounts)?;
    try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Activated: {:?}", user);

    Ok(())
}
