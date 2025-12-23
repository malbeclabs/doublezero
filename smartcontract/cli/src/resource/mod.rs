use clap::ValueEnum;
use doublezero_sdk::ResourceBlockType;

pub mod allocate;
pub mod create;
pub mod deallocate;
pub mod get;

#[derive(Clone, Copy, Debug, ValueEnum)]
pub enum ResourceExtensionType {
    DeviceTunnelBlock,
    UserTunnelBlock,
    MulticastGroupBlock,
    DzPrefixBlock,
    TunnelIds,
    LinkIds,
    SegmentRoutingIds,
}

pub fn resource_extension_to_resource_block(
    ext: ResourceExtensionType,
    associated_pubkey: Option<solana_program::pubkey::Pubkey>,
    index: Option<usize>,
) -> ResourceBlockType {
    match ext {
        ResourceExtensionType::DeviceTunnelBlock => ResourceBlockType::DeviceTunnelBlock,
        ResourceExtensionType::UserTunnelBlock => ResourceBlockType::UserTunnelBlock,
        ResourceExtensionType::MulticastGroupBlock => ResourceBlockType::MulticastGroupBlock,
        ResourceExtensionType::DzPrefixBlock => {
            let pk = associated_pubkey.unwrap_or_default();
            let idx = index.unwrap_or(0);
            ResourceBlockType::DzPrefixBlock(pk, idx)
        }
        ResourceExtensionType::TunnelIds => {
            let pk = associated_pubkey.unwrap_or_default();
            let idx = index.unwrap_or(0);
            ResourceBlockType::TunnelIds(pk, idx)
        }
        ResourceExtensionType::LinkIds => ResourceBlockType::LinkIds,
        ResourceExtensionType::SegmentRoutingIds => ResourceBlockType::SegmentRoutingIds,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_program::pubkey::Pubkey;

    #[test]
    fn test_device_tunnel_block() {
        let result = resource_extension_to_resource_block(
            ResourceExtensionType::DeviceTunnelBlock,
            None,
            None,
        );
        assert_eq!(result, ResourceBlockType::DeviceTunnelBlock);
    }

    #[test]
    fn test_user_tunnel_block() {
        let result = resource_extension_to_resource_block(
            ResourceExtensionType::UserTunnelBlock,
            None,
            None,
        );
        assert_eq!(result, ResourceBlockType::UserTunnelBlock);
    }

    #[test]
    fn test_multicast_group_block() {
        let result = resource_extension_to_resource_block(
            ResourceExtensionType::MulticastGroupBlock,
            None,
            None,
        );
        assert_eq!(result, ResourceBlockType::MulticastGroupBlock);
    }

    #[test]
    fn test_dz_prefix_block_with_values() {
        let pk = Pubkey::new_unique();
        let idx = 42;
        let result = resource_extension_to_resource_block(
            ResourceExtensionType::DzPrefixBlock,
            Some(pk),
            Some(idx),
        );
        assert_eq!(result, ResourceBlockType::DzPrefixBlock(pk, idx));
    }

    #[test]
    fn test_dz_prefix_block_defaults() {
        let result =
            resource_extension_to_resource_block(ResourceExtensionType::DzPrefixBlock, None, None);
        assert_eq!(
            result,
            ResourceBlockType::DzPrefixBlock(Pubkey::default(), 0)
        );
    }

    #[test]
    fn test_tunnel_ids_with_values() {
        let pk = Pubkey::new_unique();
        let idx = 7;
        let result = resource_extension_to_resource_block(
            ResourceExtensionType::TunnelIds,
            Some(pk),
            Some(idx),
        );
        assert_eq!(result, ResourceBlockType::TunnelIds(pk, idx));
    }

    #[test]
    fn test_tunnel_ids_defaults() {
        let result =
            resource_extension_to_resource_block(ResourceExtensionType::TunnelIds, None, None);
        assert_eq!(result, ResourceBlockType::TunnelIds(Pubkey::default(), 0));
    }

    #[test]
    fn test_link_ids() {
        let result =
            resource_extension_to_resource_block(ResourceExtensionType::LinkIds, None, None);
        assert_eq!(result, ResourceBlockType::LinkIds);
    }

    #[test]
    fn test_segment_routing_ids() {
        let result = resource_extension_to_resource_block(
            ResourceExtensionType::SegmentRoutingIds,
            None,
            None,
        );
        assert_eq!(result, ResourceBlockType::SegmentRoutingIds);
    }
}
