use crate::{doublezerocommand::CliCommand, requirements::CHECK_ID_JSON};
use clap::Args;
use std::io::Write;

#[derive(Args, Debug)]
pub struct BalanceCliCommand {}

impl BalanceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON)?;

        let balance = client.get_balance()?;

        writeln!(out, "{} SOL", balance as f64 / 1000000000.0)?;

        Ok(())
    }
}
