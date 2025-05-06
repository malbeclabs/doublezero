use clap::Args;
use doublezero_sdk::commands::allowlist::device::list::ListDeviceAllowlistCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListDeviceAllowlistArgs {}

impl ListDeviceAllowlistArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let list = ListDeviceAllowlistCommand {}.execute(client)?;

        writeln!(out, "Pubkeys:")?;
        for user in list {
            writeln!(out, "\t{}", user)?;
        }

        Ok(())
    }
}
