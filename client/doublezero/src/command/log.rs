use clap::Args;
use double_zero_sdk::DZClient;
use crate::helpers::parse_pubkey;

#[derive(Args, Debug)]
pub struct LogArgs {
    #[arg(long)]
    pubkey: String,
}

impl LogArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {

        let pubkey = parse_pubkey(&self.pubkey).expect("Invalid pubkey");

        for msg in client.get_logs(&pubkey)? {
            println!("{}", msg);
        }
        
        Ok(())
    }
}
