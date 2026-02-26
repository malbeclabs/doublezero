use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    processors::{
        resource::{allocate_id, allocate_ip, allocate_ip_from_first_available},
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
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, pubkey::Pubkey};
use std::net::Ipv4Addr;

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
