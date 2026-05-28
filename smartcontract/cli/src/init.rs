use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::globalstate::init::InitGlobalStateCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct InitCliCommand;

impl InitCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        require!(
            client,
            RequirementCheck::KEYPAIR | RequirementCheck::BALANCE
        );

        let signature = client.init_globalstate(InitGlobalStateCommand)?;
        print_signature(out, &signature)
    }
}
