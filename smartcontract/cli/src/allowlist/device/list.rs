use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::allowlist::device::list::ListDeviceAllowlistCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListDeviceAllowlistCliCommand {}

impl ListDeviceAllowlistCliCommand {
    pub fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        let list = client.list_device_allowlist(ListDeviceAllowlistCommand {})?;

        writeln!(out, "Pubkeys:")?;
        for user in list {
            writeln!(out, "\t{}", user)?;
        }

        Ok(())
    }
}
