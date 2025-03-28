use clap::Args;
use double_zero_sdk::DZClient;

use crate::helpers::print_error;
use double_zero_sdk::cli::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct InitArgs {}

impl InitArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;
        match client.initialize_globalstate() {
            Ok((pubkey, _)) => println!("Global config initialized: {}", pubkey),
            Err(e) => print_error(e),
        }

        let (_, config) = client.get_globalstate()?;

        println!("GlobalConfig: {:?}", config);

        Ok(())
    }
}
