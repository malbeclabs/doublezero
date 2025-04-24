use clap::Args;
use doublezero_sdk::*;
use doublezero_sdk::commands::allowlist::user::list::ListUserAllowlistCommand;

#[derive(Args, Debug)]
pub struct ListUserAllowlistArgs {}

impl ListUserAllowlistArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        
        let list = ListUserAllowlistCommand {}.execute(client)?;

        println!("allowlisted Pubkeys:");
        for user in list {
            println!("\t{}", user);
        }

        Ok(())
    }
}
