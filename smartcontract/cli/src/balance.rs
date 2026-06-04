use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::{require, CliContext, RequirementCheck};
use std::io::Write;

#[derive(Args, Debug)]
pub struct BalanceCliCommand;

impl BalanceCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        require!(client, RequirementCheck::KEYPAIR);

        let balance = client.get_balance()?;

        writeln!(out, "{} Credits", balance as f64 / 1_000_000_000.0)?;

        Ok(())
    }
}
