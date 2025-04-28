use clap::Args;
use doublezero_sdk::commands::allowlist::user::list::ListUserAllowlistCommand;
use doublezero_sdk::*;

#[derive(Args, Debug)]
pub struct ListUserAllowlistArgs {}

impl ListUserAllowlistArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {

        let list = ListUserAllowlistCommand {}.execute(client)?;

        println!("allowlisted Pubkeys:");
        for user in list {
            println!("\t{}", user);
        }

        Ok(())
    }
}
