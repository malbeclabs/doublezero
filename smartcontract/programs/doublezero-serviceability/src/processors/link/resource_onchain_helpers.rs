use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    processors::{
        resource::{allocate_id, allocate_ip, deallocate_id, deallocate_ip},
        validation::validate_program_account,
    },
    resource::ResourceType,
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        link::Link,
    },
};
use doublezero_program_common::types::NetworkV4;
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, pubkey::Pubkey};

/// Validate and allocate link resources from ResourceExtension accounts.
/// Allocates tunnel_net from DeviceTunnelBlock and tunnel_id from LinkIds.
pub fn validate_and_allocate_link_resources<'a>(
    program_id: &Pubkey,
    link: &mut Link,
    device_tunnel_block_ext: &AccountInfo<'a>,
    link_ids_ext: &AccountInfo<'a>,
    globalstate: &GlobalState,
) -> ProgramResult {
    // Check feature flag
    if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation) {
        return Err(DoubleZeroError::FeatureNotEnabled.into());
    }

    // Validate device_tunnel_block_ext (DeviceTunnelBlock - global)
    let (expected_device_tunnel_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
    validate_program_account!(
        device_tunnel_block_ext,
        program_id,
        writable = true,
        pda = Some(&expected_device_tunnel_pda),
        "DeviceTunnelBlock"
    );

    // Validate link_ids_ext (LinkIds - global)
    let (expected_link_ids_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::LinkIds);
    validate_program_account!(
        link_ids_ext,
        program_id,
        writable = true,
        pda = Some(&expected_link_ids_pda),
        "LinkIds"
    );

    // Allocate tunnel_net from global DeviceTunnelBlock (skip if already allocated)
    if link.tunnel_net == NetworkV4::default() {
        link.tunnel_net = allocate_ip(device_tunnel_block_ext, 2)?;
    }

    // Allocate tunnel_id from global LinkIds (skip if already allocated)
    if link.tunnel_id == 0 {
        link.tunnel_id = allocate_id(link_ids_ext)?;
    }

    Ok(())
}

/// Validate and deallocate link resources back to ResourceExtension accounts.
/// Deallocates tunnel_net to DeviceTunnelBlock and tunnel_id to LinkIds.
/// Deallocation is idempotent.
pub fn validate_and_deallocate_link_resources<'a>(
    program_id: &Pubkey,
    link: &Link,
    device_tunnel_block_ext: &AccountInfo<'a>,
    link_ids_ext: &AccountInfo<'a>,
    globalstate: &GlobalState,
) -> ProgramResult {
    // Check feature flag
    if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation) {
        return Err(DoubleZeroError::FeatureNotEnabled.into());
    }

    // Validate device_tunnel_block_ext (DeviceTunnelBlock - global)
    let (expected_device_tunnel_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
    validate_program_account!(
        device_tunnel_block_ext,
        program_id,
        writable = true,
        pda = Some(&expected_device_tunnel_pda),
        "DeviceTunnelBlock"
    );

    // Validate link_ids_ext (LinkIds - global)
    let (expected_link_ids_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::LinkIds);
    validate_program_account!(
        link_ids_ext,
        program_id,
        writable = true,
        pda = Some(&expected_link_ids_pda),
        "LinkIds"
    );

    // Deallocate tunnel_net from global DeviceTunnelBlock
    deallocate_ip(device_tunnel_block_ext, link.tunnel_net);

    // Deallocate tunnel_id from global LinkIds
    deallocate_id(link_ids_ext, link.tunnel_id);

    Ok(())
}
