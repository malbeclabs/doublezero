use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::resource::deallocate::DeallocateResourceCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeallocateResourceCliCommand {
    #[clap(long)]
    pub network: String,
}

impl DeallocateResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let signature = client.deallocate_resource(DeallocateResourceCommand {
            network: self.network.parse().unwrap(), // TODO
        })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}
