use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::migrate::MigrateCommand;
use serde_json::to_writer_pretty;
use std::io::Write;

#[derive(Args, Debug)]
pub struct MigrateUserPdaCliCommand {}

impl MigrateUserPdaCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let _ = client.migrate(MigrateCommand {})?;
        to_writer_pretty(out, &"Migration completed successfully")?;
        Ok(())
    }
}
