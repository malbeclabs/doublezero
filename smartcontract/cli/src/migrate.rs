use clap::Args;
use doublezero_sdk::commands::migrate::MigrateCommand;
use serde_json::to_writer_pretty;
use std::io::Write;

use crate::doublezerocommand::CliCommand;

#[derive(Args, Debug)]
pub struct MigrateCliCommand {}

impl MigrateCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let _ = client.migrate(MigrateCommand {})?;
        to_writer_pretty(out, &"Migration completed successfully")?;
        Ok(())
    }
}
