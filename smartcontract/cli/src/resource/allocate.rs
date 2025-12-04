use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::resource::allocate::AllocateResourceCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct AllocateResourceCliCommand {}

impl AllocateResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let signature = client.allocate_resource(AllocateResourceCommand {})?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}
