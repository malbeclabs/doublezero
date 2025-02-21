use crate::{helpers::{parse_pubkey, print_error}, requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON}};
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

        let device = client.get_device(&pubkey)?;
        match client.delete_device(device.index) {
            Ok(_) => println!("Device deleted"),
            Err(e) => print_error(e),
        }

        Ok(())
    }
}
