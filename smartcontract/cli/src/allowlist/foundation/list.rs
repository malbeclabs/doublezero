use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::allowlist::foundation::list::ListFoundationAllowlistCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListFoundationAllowlistCliCommand {}

impl ListFoundationAllowlistCliCommand {
    pub fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        let list = client.list_foundation_allowlist(ListFoundationAllowlistCommand {})?;

        writeln!(out, "Pubkeys:")?;
        for user in list {
            writeln!(out, "\t{}", user)?;
        }

        Ok(())
    }
}
