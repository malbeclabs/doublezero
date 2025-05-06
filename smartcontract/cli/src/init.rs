use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::globalstate::init::InitGlobalStateCommand;
use doublezero_sdk::DoubleZeroClient;
use std::io::Write;

#[derive(Args, Debug)]
pub struct InitArgs {}

impl InitArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let signature = InitGlobalStateCommand {}.execute(client)?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}
