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
