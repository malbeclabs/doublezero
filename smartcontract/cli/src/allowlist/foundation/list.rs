use clap::Args;
use doublezero_sdk::commands::allowlist::foundation::list::ListFoundationAllowlistCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListFoundationAllowlistCliCommand {}

impl ListFoundationAllowlistCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let list = ListFoundationAllowlistCommand {}.execute(client)?;

        writeln!(out, "Pubkeys:")?;
        for user in list {
            writeln!(out, "\t{}", user)?;
        }

        Ok(())
    }
}
