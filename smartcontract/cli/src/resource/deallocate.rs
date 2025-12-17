use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::{
    commands::{device::get::GetDeviceCommand, resource::deallocate::DeallocateResourceCommand},
    IdOrIp, ResourceBlockType,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeallocateResourceCliCommand {
    // Type of resource extension to allocate
    #[arg(long)]
    pub resource_extension_type: super::ResourceExtensionType,
    // Associated public key (only for DzPrefixBlock)
    #[arg(long)]
    pub associated_pubkey: Option<String>,
    // Index (only for DzPrefixBlock)
    #[arg(long)]
    pub index: Option<usize>,
    // Requested value to deallocate
    #[arg(long)]
    pub value: String,
}

impl From<DeallocateResourceCliCommand> for DeallocateResourceCommand {
    fn from(cmd: DeallocateResourceCliCommand) -> Self {
        let resource_block_type = super::resource_extension_to_resource_block(
            cmd.resource_extension_type,
            cmd.associated_pubkey.as_ref().and_then(|s| s.parse().ok()),
            cmd.index,
        );

        let value = match cmd.resource_extension_type {
            super::ResourceExtensionType::DeviceTunnelBlock
            | super::ResourceExtensionType::UserTunnelBlock
            | super::ResourceExtensionType::MulticastGroupBlock
            | super::ResourceExtensionType::DzPrefixBlock => IdOrIp::Ip(
                cmd.value
                    .parse::<NetworkV4>()
                    .expect("Failed to parse IP address")
                    .into(),
            ),
            super::ResourceExtensionType::TunnelIds
            | super::ResourceExtensionType::LinkIds
            | super::ResourceExtensionType::SegmentRoutingIds => {
                IdOrIp::Id(cmd.value.parse::<u16>().expect("Failed to parse ID").into())
            }
        };

        DeallocateResourceCommand {
            resource_block_type,
            value,
        }
    }
}

impl DeallocateResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let args: DeallocateResourceCommand = self.into();

        match args.resource_block_type {
            ResourceBlockType::DzPrefixBlock(pk, index)
            | ResourceBlockType::TunnelIds(pk, index) => {
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

        let signature = client.deallocate_resource(args)?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}
