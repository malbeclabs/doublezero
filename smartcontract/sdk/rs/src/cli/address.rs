use clap::Args;
use solana_sdk::signer::Signer;

use crate::{cli::requirements::{check_requirements, CHECK_ID_JSON}, get_doublezero_pubkey, DZClient};

#[derive(Args, Debug)]
pub struct AddressArgs {}

impl AddressArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON)?;

        match get_doublezero_pubkey() {
            Some(pubkey) => {
                println!("{}", pubkey.pubkey());
            }
            None => {
                eprintln!("Unable to read the Pubkey");
            }
        }
        Ok(())
    }
}
