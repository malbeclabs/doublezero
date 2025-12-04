use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::{commands::resource::get::GetResourceCommand, IpBlockType};
use std::io::Write;
use tabled::{Table, Tabled};

#[derive(Args, Debug)]
pub struct GetResourceCliCommand {
    // Type of resource extension to allocate
    #[arg(long)]
    pub resource_extension_type: super::ResourceExtensionType,
    // Associated public key (only for DzPrefixBlock)
    #[arg(long)]
    pub associated_pubkey: Option<String>,
    // Index (only for DzPrefixBlock)
    #[arg(long)]
    pub index: Option<usize>,
}

impl From<GetResourceCliCommand> for GetResourceCommand {
    fn from(cmd: GetResourceCliCommand) -> Self {
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

        GetResourceCommand { ip_block_type }
    }
}

#[derive(Tabled)]
pub struct ResourceDisplay {
    #[tabled(rename = "Allocated Resources")]
    pub ip: String,
}

impl GetResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, resource_extension) = client.get_resource(self.into())?;

        let resource_displays = resource_extension
            .iter_allocated_ips()
            .into_iter()
            .map(|ip| ResourceDisplay { ip: ip.to_string() })
            .collect::<Vec<_>>();
        let table = Table::new(resource_displays).to_string();
        writeln!(out, "{table}")?;

        Ok(())
    }
}
