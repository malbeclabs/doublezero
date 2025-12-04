use clap::ValueEnum;

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
}
