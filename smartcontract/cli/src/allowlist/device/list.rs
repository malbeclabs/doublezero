use clap::Args;
use doublezero_sdk::*;
use doublezero_sdk::commands::allowlist::device::list::ListDeviceAllowlistCommand;

#[derive(Args, Debug)]
pub struct ListDeviceAllowlistArgs {}

impl ListDeviceAllowlistArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {

        let list = ListDeviceAllowlistCommand{}.execute(client)?;

        println!("Pubkeys:");
        for user in list {
            println!("\t{}", user);
        }

        Ok(())
    }
}
