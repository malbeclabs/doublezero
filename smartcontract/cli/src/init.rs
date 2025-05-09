use crate::doublezerocommand::CliCommand;
use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::globalstate::init::InitGlobalStateCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct InitCliCommand {}

impl InitCliCommand {
    pub fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let signature = client.init_global_state(InitGlobalStateCommand {})?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}
