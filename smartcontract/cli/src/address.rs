use crate::{doublezerocommand::CliCommand, requirements::CHECK_ID_JSON};
use clap::Args;
use std::io::Write;

#[derive(Args, Debug)]
pub struct AddressCliCommand;

impl AddressCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON)?;

        writeln!(out, "{}", &client.get_payer())?;

        Ok(())
    }
}
