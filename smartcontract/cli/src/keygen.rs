use clap::Args;
use colored::Colorize;
use double_zero_sdk::{create_new_pubkey_user, DZClient};
use solana_sdk::signer::Signer;

#[derive(Args, Debug)]
pub struct KeyGenArgs {
    #[arg(short, default_value = "false", help = "Force keypair generation")]
    force: bool,
}

impl KeyGenArgs {
    pub async fn execute(self, _: &DZClient) -> eyre::Result<()> {

        match create_new_pubkey_user(self.force) {
            Ok( keypair) => {
                println!("{}: {}", "Pubkey".green(), keypair.pubkey());
            }
            Err(e) => {
                eprintln!("{}: {}", "Error".red(), e);
            }
        };
        
        Ok(())
    }
}
