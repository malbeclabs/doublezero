use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::{
    commands::{device::get::GetDeviceCommand, resource::allocate::AllocateResourceCommand},
    IpBlockType,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct AllocateResourceCliCommand {
    // Type of resource extension to allocate
    #[arg(long)]
    pub resource_extension_type: super::ResourceExtensionType,
    // Associated public key (only for DzPrefixBlock)
    #[arg(long)]
    pub associated_pubkey: Option<String>,
    // Index (only for DzPrefixBlock)
    #[arg(long)]
    pub index: Option<usize>,
    // Requested allocation (optional)
    #[arg(long)]
    pub requested_allocation: Option<String>,
}

impl From<AllocateResourceCliCommand> for AllocateResourceCommand {
    fn from(cmd: AllocateResourceCliCommand) -> Self {
        let ip_block_type = match cmd.resource_extension_type {
            super::ResourceExtensionType::DeviceTunnelBlock => IpBlockType::DeviceTunnelBlock,
            super::ResourceExtensionType::UserTunnelBlock => IpBlockType::UserTunnelBlock,
            super::ResourceExtensionType::MulticastGroupBlock => IpBlockType::MulticastGroupBlock,
            super::ResourceExtensionType::DzPrefixBlock => {
                let pk = cmd
                    .associated_pubkey
                    .as_ref()
                    .and_then(|s| s.parse().ok())
                    .unwrap_or_default();
                let index = cmd.index.unwrap_or(0);
                IpBlockType::DzPrefixBlock(pk, index)
            }
        };

        let requested_network = cmd
            .requested_allocation
            .as_ref()
            .and_then(|s| s.parse().ok());

        AllocateResourceCommand {
            ip_block_type,
            requested_network,
        }
    }
}

impl AllocateResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let args: AllocateResourceCommand = self.into();

        match args.ip_block_type {
            IpBlockType::DzPrefixBlock(pk, index) => {
                let get_device_cmd = GetDeviceCommand {
                    pubkey_or_code: pk.to_string(),
                };
                let (_device_pk, device) = client.get_device(get_device_cmd)?;
                if device.dz_prefixes.len() <= index {
                    return Err(eyre::eyre!(
                        "Device does not have a DzPrefixBlock at index {}",
                        index
                    ));
                }
            }
            _ => {}
        }

        let signature = client.allocate_resource(args)?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}
