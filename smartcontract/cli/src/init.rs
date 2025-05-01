use clap::Args;
use doublezero_sdk::DZClient;

use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use doublezero_sdk::commands::globalstate::init::InitGlobalStateCommand;

#[derive(Args, Debug)]
pub struct InitArgs {}

impl InitArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;


        let signature = InitGlobalStateCommand{}.execute(client)?;
        println!("Signature: {}", signature);

        Ok(())
    }
}
