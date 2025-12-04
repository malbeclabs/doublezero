use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::resource::get::GetResourceCommand;
use std::io::Write;
use tabled::{Table, Tabled};

#[derive(Args, Debug)]
pub struct GetResourceCliCommand {}

#[derive(Tabled)]
pub struct ResourceDisplay {
    #[tabled(rename = "IP")]
    pub ip: String,
}

impl GetResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, resource_extension) = client.get_resource(GetResourceCommand {})?;

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
