use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::globalstate::init::InitGlobalStateCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct InitCliCommand;

impl InitCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let signature = client.init_globalstate(InitGlobalStateCommand)?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}
