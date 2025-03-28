use crate::helpers::{parse_pubkey, print_error};
use double_zero_sdk::cli::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

use clap::Args;
use double_zero_sdk::*;

#[derive(Args, Debug)]
pub struct DeleteLocationArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteLocationArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = parse_pubkey(&self.pubkey).expect("Invalid pubkey");

        let location = client.get_location(&pubkey)?;
        match client.delete_location(location.index) {
            Ok(_) => println!("Location deleted"),
            Err(e) => print_error(e),
        }

        Ok(())
    }
}
