use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::allowlist::user::list::ListUserAllowlistCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListUserAllowlistCliCommand {}

impl ListUserAllowlistCliCommand {
    pub fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        let list = client.list_user_allowlist(ListUserAllowlistCommand {})?;

        writeln!(out, "allowlisted Pubkeys:")?;
        for user in list {
            writeln!(out, "\t{}", user)?;
        }

        Ok(())
    }
}
