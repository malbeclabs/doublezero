use clap::Args;
use doublezero_sdk::*;
use doublezero_sdk::commands::allowlist::foundation::list::ListFoundationAllowlistCommand;

#[derive(Args, Debug)]
pub struct ListFoundationAllowlistArgs {}

impl ListFoundationAllowlistArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {

        let list = ListFoundationAllowlistCommand{}.execute(client)?;

        println!("Pubkeys:");
        for user in list {
            println!("\t{}", user);
        }

        Ok(())
    }
}
