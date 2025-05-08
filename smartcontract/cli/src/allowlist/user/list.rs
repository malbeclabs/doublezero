use clap::Args;
use doublezero_sdk::commands::allowlist::user::list::ListUserAllowlistCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListUserAllowlistCliCommand {}

impl ListUserAllowlistCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let list = ListUserAllowlistCommand {}.execute(client)?;

        writeln!(out, "allowlisted Pubkeys:")?;
        for user in list {
            writeln!(out, "\t{}", user)?;
        }

        Ok(())
    }
}
