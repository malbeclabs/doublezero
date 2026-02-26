use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    resource::{IdOrIp, ResourceType},
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        resource_extension::ResourceExtensionBorrowed,
        user::User,
    },
};
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, pubkey::Pubkey};
use std::net::Ipv4Addr;

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

    // Validate multicast_publisher_block_ext if provided
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
                    let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
                    deallocated = resource.deallocate(&IdOrIp::Ip(dz_ip_net));
                    #[cfg(test)]
                    msg!(
                        "Deallocated dz_ip {} from DzPrefixBlock {:?}: {}",
                        dz_ip_net,
                        dz_prefix_account.key,
                        deallocated
                    );
                    if deallocated {
                        break;
                    }
                }
            }
        }
    }

    Ok(())
}
