use crate::doublezerocommand::CliCommand;
use clap::ValueEnum;
use doublezero_sdk::{commands::device::get::GetDeviceCommand, ResourceType as SdkResourceType};

pub mod allocate;
pub mod close;
pub mod create;
pub mod deallocate;
pub mod get;
pub mod verify;

#[derive(Clone, Copy, Debug, ValueEnum)]
pub enum ResourceType {
    DeviceTunnelBlock,
    UserTunnelBlock,
    MulticastGroupBlock,
    DzPrefixBlock,
    TunnelIds,
    LinkIds,
    SegmentRoutingIds,
}

pub fn resource_type_from(
    ext: ResourceType,
    associated_pubkey: Option<solana_program::pubkey::Pubkey>,
    index: Option<usize>,
) -> SdkResourceType {
    match ext {
        ResourceType::DeviceTunnelBlock => SdkResourceType::DeviceTunnelBlock,
        ResourceType::UserTunnelBlock => SdkResourceType::UserTunnelBlock,
        ResourceType::MulticastGroupBlock => SdkResourceType::MulticastGroupBlock,
        ResourceType::DzPrefixBlock => {
            let pk = associated_pubkey.unwrap_or_default();
            let idx = index.unwrap_or(0);
            SdkResourceType::DzPrefixBlock(pk, idx)
        }
        ResourceType::TunnelIds => {
            let pk = associated_pubkey.unwrap_or_default();
            let idx = index.unwrap_or(0);
            SdkResourceType::TunnelIds(pk, idx)
        }
        ResourceType::LinkIds => SdkResourceType::LinkIds,
        ResourceType::SegmentRoutingIds => SdkResourceType::SegmentRoutingIds,
    }
}

fn check_device_if_needed(
    resource_type: &SdkResourceType,
    client: &impl CliCommand,
) -> eyre::Result<()> {
    match resource_type {
        SdkResourceType::DzPrefixBlock(pk, index) | SdkResourceType::TunnelIds(pk, index) => {
            let get_device_cmd = GetDeviceCommand {
                pubkey_or_code: pk.to_string(),
            };
            let (_device_pk, device) = client.get_device(get_device_cmd)?;
            if device.dz_prefixes.len() <= *index {
                return Err(eyre::eyre!(
                    "Device does not have a DzPrefixBlock at index {}",
                    index
                ));
            }
        }
        _ => {}
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_program::pubkey::Pubkey;

    #[test]
    fn test_device_tunnel_block() {
        let result = resource_type_from(ResourceType::DeviceTunnelBlock, None, None);
        assert_eq!(result, SdkResourceType::DeviceTunnelBlock);
    }

    #[test]
    fn test_user_tunnel_block() {
        let result = resource_type_from(ResourceType::UserTunnelBlock, None, None);
        assert_eq!(result, SdkResourceType::UserTunnelBlock);
    }

    #[test]
    fn test_multicast_group_block() {
        let result = resource_type_from(ResourceType::MulticastGroupBlock, None, None);
        assert_eq!(result, SdkResourceType::MulticastGroupBlock);
    }

    #[test]
    fn test_dz_prefix_block_with_values() {
        let pk = Pubkey::new_unique();
        let idx = 42;
        let result = resource_type_from(ResourceType::DzPrefixBlock, Some(pk), Some(idx));
        assert_eq!(result, SdkResourceType::DzPrefixBlock(pk, idx));
    }

    #[test]
    fn test_dz_prefix_block_defaults() {
        let result = resource_type_from(ResourceType::DzPrefixBlock, None, None);
        assert_eq!(result, SdkResourceType::DzPrefixBlock(Pubkey::default(), 0));
    }

    #[test]
    fn test_tunnel_ids_with_values() {
        let pk = Pubkey::new_unique();
        let idx = 7;
        let result = resource_type_from(ResourceType::TunnelIds, Some(pk), Some(idx));
        assert_eq!(result, SdkResourceType::TunnelIds(pk, idx));
    }

    #[test]
    fn test_tunnel_ids_defaults() {
        let result = resource_type_from(ResourceType::TunnelIds, None, None);
        assert_eq!(result, SdkResourceType::TunnelIds(Pubkey::default(), 0));
    }

    #[test]
    fn test_link_ids() {
        let result = resource_type_from(ResourceType::LinkIds, None, None);
        assert_eq!(result, SdkResourceType::LinkIds);
    }

    #[test]
    fn test_segment_routing_ids() {
        let result = resource_type_from(ResourceType::SegmentRoutingIds, None, None);
        assert_eq!(result, SdkResourceType::SegmentRoutingIds);
    }
}
