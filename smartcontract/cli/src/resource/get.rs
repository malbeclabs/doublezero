use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::resource::get::GetResourceCommand;
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
        let resource_block_type = super::resource_extension_to_resource_block(
            cmd.resource_extension_type,
            cmd.associated_pubkey.as_ref().and_then(|s| s.parse().ok()),
            cmd.index,
        );

        GetResourceCommand {
            resource_block_type,
        }
    }
}

#[derive(Tabled)]
pub struct ResourceDisplay {
    #[tabled(rename = "Allocated Resources")]
    pub resource: String,
}

impl GetResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, resource_extension) = client.get_resource(self.into())?;

        let resource_displays = resource_extension
            .iter_allocated()
            .into_iter()
            .map(|res| ResourceDisplay {
                resource: res.to_string(),
            })
            .collect::<Vec<_>>();
        let table = Table::new(resource_displays).to_string();
        writeln!(out, "{table}")?;

        Ok(())
    }
}
