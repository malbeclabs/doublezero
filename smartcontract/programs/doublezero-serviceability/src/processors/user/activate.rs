use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    processors::{
        resource::{allocate_id, allocate_ip, allocate_ip_from_first_available},
        validation::validate_program_account,
    },
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        accesspass::AccessPass,
        globalstate::GlobalState,
        user::{TunnelFlags, User, UserStatus, UserType},
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
    /// Number of DzPrefixBlock accounts passed for on-chain allocation.
    /// When 0, legacy behavior is used (values from args). When > 0, on-chain allocation is used.
    #[incremental(default = 0)]
    pub dz_prefix_count: u8,
    /// Tunnel endpoint IP (device-side GRE endpoint). 0.0.0.0 means use device.public_ip for backwards compatibility.
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub tunnel_endpoint: Ipv4Addr,
}

impl fmt::Debug for UserActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "tunnel_id: {}, tunnel_net: {}, dz_ip: {}, dz_prefix_count: {}, tunnel_endpoint: {}",
            self.tunnel_id,
            &self.tunnel_net,
            &self.dz_ip,
            self.dz_prefix_count,
            &self.tunnel_endpoint,
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
    // Account layout WITH ResourceExtension (dz_prefix_count > 0):
    //   [user, accesspass, globalstate, global_resource_ext, multicast_publisher_block_ext, device_tunnel_ids_ext, dz_prefix_ext_0..N, payer, system]
    // Account layout WITHOUT (legacy, dz_prefix_count == 0):
    //   [user, accesspass, globalstate, payer, system]
    let resource_extension_accounts = if value.dz_prefix_count > 0 {
        let global_resource_ext = next_account_info(accounts_iter)?; // UserTunnelBlock
        let multicast_publisher_block_ext = next_account_info(accounts_iter)?; // MulticastPublisherBlock
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

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_user({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(
        user_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
        "User"
    );
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    validate_program_account!(
        accesspass_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "AccessPass"
    );
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "GlobalState"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

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
    if let Some((
        global_resource_ext,
        multicast_publisher_block_ext,
        device_tunnel_ids_ext,
        dz_prefix_accounts,
    )) = resource_extension_accounts
    {
        // Validate UserTunnelBlock
        let (expected_user_tunnel_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
        validate_program_account!(
            global_resource_ext,
            program_id,
            writable = true,
            pda = Some(&expected_user_tunnel_pda),
            "UserTunnelBlock"
        );

        // Only validate MulticastPublisherBlock for multicast publishers
        // (non-publishers don't use this account, so it may not be initialized yet)
        let is_publisher = user.user_type == UserType::Multicast && !user.publishers.is_empty();
        if is_publisher {
            let (expected_multicast_publisher_pda, _, _) =
                get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
            validate_program_account!(
                multicast_publisher_block_ext,
                program_id,
                writable = true,
                pda = Some(&expected_multicast_publisher_pda),
                "MulticastPublisherBlock"
            );
        }

        // Validate TunnelIds
        let (expected_tunnel_ids_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::TunnelIds(user.device_pk, 0));
        validate_program_account!(
            device_tunnel_ids_ext,
            program_id,
            writable = true,
            pda = Some(&expected_tunnel_ids_pda),
            "TunnelIds"
        );

        // Validate all DzPrefixBlock accounts
        for (idx, dz_prefix_account) in dz_prefix_accounts.iter().enumerate() {
            let (expected_dz_prefix_pda, _, _) = get_resource_extension_pda(
                program_id,
                ResourceType::DzPrefixBlock(user.device_pk, idx),
            );
            validate_program_account!(
                dz_prefix_account,
                program_id,
                writable = true,
                pda = Some(&expected_dz_prefix_pda),
                &format!("DzPrefixBlock[{idx}]")
            );
        }

        // Allocate tunnel_net from global UserTunnelBlock (only if not already allocated)
        // This check handles re-activation (Updating status) where resources are already assigned
        if user.tunnel_net == NetworkV4::default() {
            user.tunnel_net = allocate_ip(global_resource_ext, 2)?;
        }

        // Allocate tunnel_id from device TunnelIds (only if not already allocated)
        if user.tunnel_id == 0 {
            user.tunnel_id = allocate_id(device_tunnel_ids_ext)?;
        }

        // Conditionally allocate dz_ip based on user_type (matching activator behavior)
        let need_dz_ip = match user.user_type {
            UserType::IBRLWithAllocatedIP | UserType::EdgeFiltering => true,
            UserType::IBRL => false,
            UserType::Multicast => !user.publishers.is_empty(),
        };

        // Only allocate dz_ip if needed AND not already allocated
        // - dz_ip == UNSPECIFIED: new user, never had dz_ip allocated
        // - dz_ip == client_ip: Multicast user that didn't need dz_ip before (no publishers)
        // If dz_ip is already a dedicated IP (not UNSPECIFIED or client_ip), keep it
        if need_dz_ip && (user.dz_ip == Ipv4Addr::UNSPECIFIED || user.dz_ip == user.client_ip) {
            let allocated_dz_ip =
                if user.user_type == UserType::Multicast && !user.publishers.is_empty() {
                    // Multicast publishers: allocate from global MulticastPublisherBlock
                    allocate_ip(multicast_publisher_block_ext, 1)?.ip()
                } else {
                    // EdgeFiltering/IBRL: allocate from device DzPrefixBlock
                    allocate_ip_from_first_available(&dz_prefix_accounts)?
                };

            user.dz_ip = allocated_dz_ip;
        } else if !need_dz_ip && user.dz_ip == Ipv4Addr::UNSPECIFIED {
            // First activation for user that doesn't need dz_ip: use client_ip
            user.dz_ip = user.client_ip;
        }
        // Otherwise keep existing dz_ip (already allocated or client_ip)

        // Set tunnel_endpoint from args (device's public_ip, passed by activator)
        user.tunnel_endpoint = value.tunnel_endpoint;
    } else {
        // Legacy behavior: use provided args
        user.tunnel_id = value.tunnel_id;
        user.tunnel_net = value.tunnel_net;
        user.dz_ip = value.dz_ip;
        user.tunnel_endpoint = value.tunnel_endpoint;
    }

    // Record publisher role on FIRST activation only (Pending → Activated).
    // On re-activation (Updating → Activated), leave the flag unchanged: the publishers list
    // may be empty after an unsubscribe, but the device counter that was incremented at
    // creation time hasn't changed and must still be decremented at delete time.
    if user.status == UserStatus::Pending
        && user.user_type == UserType::Multicast
        && !user.publishers.is_empty()
    {
        user.tunnel_flags = TunnelFlags::set(user.tunnel_flags, TunnelFlags::CreatedAsPublisher);
    }

    user.try_activate(&mut accesspass)?;

    try_acc_write(&user, user_account, payer_account, accounts)?;
    try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Activated: {:?}", user);

    Ok(())
}
