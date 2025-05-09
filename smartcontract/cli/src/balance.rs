use crate::doublezerocommand::CliCommand;
use crate::requirements::{check_requirements, CHECK_ID_JSON};
use clap::Args;
use std::io::Write;

#[derive(Args, Debug)]
pub struct BalanceCliCommand {}

impl BalanceCliCommand {
    pub fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON)?;

        let balance = client.get_balance()?;

        writeln!(out, "{} SOL", balance as f64 / 1000000000.0)?;

        Ok(())
    }
}
