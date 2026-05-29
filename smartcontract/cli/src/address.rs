use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::{require, CliContext, RequirementCheck};
use std::io::Write;

#[derive(Args, Debug)]
pub struct AddressCliCommand;

impl AddressCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        require!(client, RequirementCheck::KEYPAIR);

        writeln!(out, "{}", &client.get_payer())?;

        Ok(())
    }
}
