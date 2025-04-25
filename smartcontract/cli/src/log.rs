use crate::helpers::parse_pubkey;
use clap::Args;
use doublezero_sdk::DZClient;

#[derive(Args, Debug)]
pub struct LogArgs {
    #[arg(long)]
    pubkey: String,
}

impl LogArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        let pubkey = parse_pubkey(&self.pubkey).expect("Invalid pubkey");

        for msg in client.get_logs(&pubkey)? {
            println!("{}", msg);
        }

        Ok(())
    }
}
