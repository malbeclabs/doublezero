use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    processors::{
        resource::{
            allocate_id, allocate_ip, allocate_ip_from_first_available, deallocate_id,
            deallocate_ip,
        },
        validation::validate_program_account,
    },
    resource::ResourceType,
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        user::{User, UserType},
    },
};
use doublezero_program_common::types::NetworkV4;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

/// Type alias for parsed resource extension accounts.
pub type ResourceExtensionAccounts<'a, 'b> = (
    &'b AccountInfo<'a>,
    &'b AccountInfo<'a>,
    &'b AccountInfo<'a>,
    Vec<&'b AccountInfo<'a>>,
);

/// Parse optional ResourceExtension accounts from the accounts iterator.
/// Returns `Some(...)` when `dz_prefix_count > 0`, `None` otherwise.
pub fn parse_resource_extension_accounts<'a, 'b>(
    accounts_iter: &mut std::slice::Iter<'b, AccountInfo<'a>>,
    dz_prefix_count: u8,
) -> Result<Option<ResourceExtensionAccounts<'a, 'b>>, solana_program::program_error::ProgramError>
{
    if dz_prefix_count > 0 {
        let user_tunnel_block_ext = next_account_info(accounts_iter)?;
        let multicast_publisher_block_ext = next_account_info(accounts_iter)?;
        let device_tunnel_ids_ext = next_account_info(accounts_iter)?;

        let mut dz_prefix_accounts = Vec::with_capacity(dz_prefix_count as usize);
        for _ in 0..dz_prefix_count {
            dz_prefix_accounts.push(next_account_info(accounts_iter)?);
        }

        Ok(Some((
            user_tunnel_block_ext,
            multicast_publisher_block_ext,
            device_tunnel_ids_ext,
            dz_prefix_accounts,
        )))
    } else {
        Ok(None)
    }
}

/// Validate and allocate user resources from ResourceExtension accounts.
/// Allocates tunnel_net from UserTunnelBlock, tunnel_id from TunnelIds,
/// and conditionally dz_ip from MulticastPublisherBlock or DzPrefixBlock.
pub fn validate_and_allocate_user_resources<'a>(
    program_id: &Pubkey,
    user: &mut User,
    global_resource_ext: &AccountInfo<'a>,
    multicast_publisher_block_ext: &AccountInfo<'a>,
    device_tunnel_ids_ext: &AccountInfo<'a>,
    dz_prefix_accounts: &[&AccountInfo<'a>],
    globalstate: &GlobalState,
) -> ProgramResult {
    // Check feature flag
    if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation) {
        return Err(DoubleZeroError::FeatureNotEnabled.into());
    }

    // Validate global_resource_ext (UserTunnelBlock)
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

    // Validate device_tunnel_ids_ext (TunnelIds)
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

    // Allocate tunnel_net from global UserTunnelBlock (skip if already allocated)
    if user.tunnel_net == NetworkV4::default() {
        user.tunnel_net = allocate_ip(global_resource_ext, 2)?;
    }

    // Allocate tunnel_id from device TunnelIds (skip if already allocated)
    if user.tunnel_id == 0 {
        user.tunnel_id = allocate_id(device_tunnel_ids_ext)?;
    }

    // Conditionally allocate dz_ip based on user_type
    let need_dz_ip = match user.user_type {
        UserType::IBRLWithAllocatedIP | UserType::EdgeFiltering => true,
        UserType::IBRL => false,
        UserType::Multicast => !user.publishers.is_empty(),
    };

    if need_dz_ip && (user.dz_ip == Ipv4Addr::UNSPECIFIED || user.dz_ip == user.client_ip) {
        let allocated_dz_ip =
            if user.user_type == UserType::Multicast && !user.publishers.is_empty() {
                // Multicast publishers: allocate from global MulticastPublisherBlock
                allocate_ip(multicast_publisher_block_ext, 1)?.ip()
            } else {
                // EdgeFiltering/IBRLWithAllocatedIP: allocate from device DzPrefixBlock
                allocate_ip_from_first_available(dz_prefix_accounts)?
            };

        user.dz_ip = allocated_dz_ip;
    } else if !need_dz_ip && user.dz_ip == Ipv4Addr::UNSPECIFIED {
        // First activation for user that doesn't need dz_ip: use client_ip
        user.dz_ip = user.client_ip;
    }

    Ok(())
}

/// Validate and deallocate user resources back to ResourceExtension accounts.
/// Deallocates tunnel_net, tunnel_id, and conditionally dz_ip.
/// Deallocation is idempotent.
pub fn validate_and_deallocate_user_resources<'a>(
    program_id: &Pubkey,
    user: &User,
    global_resource_ext: &AccountInfo<'a>,
    multicast_publisher_block_ext: Option<&AccountInfo<'a>>,
    device_tunnel_ids_ext: &AccountInfo<'a>,
    dz_prefix_accounts: &[&AccountInfo<'a>],
    globalstate: &GlobalState,
) -> ProgramResult {
    // Check feature flag
    if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation) {
        return Err(DoubleZeroError::FeatureNotEnabled.into());
    }

    // Validate global_resource_ext (UserTunnelBlock)
    let (expected_user_tunnel_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
    validate_program_account!(
        global_resource_ext,
        program_id,
        writable = true,
        pda = Some(&expected_user_tunnel_pda),
        "UserTunnelBlock"
    );

    // Validate multicast_publisher_block_ext if provided
    if let Some(multicast_publisher_ext) = multicast_publisher_block_ext {
        let (expected_multicast_publisher_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
        validate_program_account!(
            multicast_publisher_ext,
            program_id,
            writable = true,
            pda = Some(&expected_multicast_publisher_pda),
            "MulticastPublisherBlock"
        );
    }

    // Validate device_tunnel_ids_ext (TunnelIds)
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

    // Deallocate tunnel_net from global UserTunnelBlock
    deallocate_ip(global_resource_ext, user.tunnel_net);

    // Deallocate tunnel_id from device TunnelIds
    deallocate_id(device_tunnel_ids_ext, user.tunnel_id);

    // Deallocate dz_ip (try MulticastPublisherBlock first, then DzPrefixBlock)
    // Only deallocate if dz_ip is allocated (not client_ip and not UNSPECIFIED)
    if user.dz_ip != user.client_ip && user.dz_ip != Ipv4Addr::UNSPECIFIED {
        let mut deallocated = false;

        // Try MulticastPublisherBlock first (for publishers)
        if let Some(multicast_publisher_ext) = multicast_publisher_block_ext {
            deallocated = deallocate_ip(multicast_publisher_ext, user.dz_ip.into());
        }

        // Fall back to DzPrefixBlock if not in MulticastPublisherBlock
        if !deallocated {
            for dz_prefix_account in dz_prefix_accounts.iter() {
                deallocated = deallocate_ip(dz_prefix_account, user.dz_ip.into());
                if deallocated {
                    break;
                }
            }
        }
    }

    Ok(())
}
