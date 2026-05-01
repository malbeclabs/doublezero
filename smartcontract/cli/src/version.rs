use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::programconfig::get::GetProgramConfigCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct VersionCliCommand;

impl VersionCliCommand {
    pub fn execute<C: CliCommand, W: Write>(
        self,
        client: &C,
        local_version: &str,
        out: &mut W,
    ) -> eyre::Result<()> {
        writeln!(out, "client version:       {local_version}")?;
        if let Ok((_, pconfig)) = client.get_program_config(GetProgramConfigCommand) {
            writeln!(out, "program version:      {}", pconfig.version)?;
            writeln!(
                out,
                "min required version: {}",
                pconfig.min_compatible_version
            )?;
        }
        Ok(())
    }
}
