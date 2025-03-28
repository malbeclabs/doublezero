use clap::Args;
use double_zero_sdk::*;

#[derive(Args, Debug)]
pub struct GetExchangeArgs {
    #[arg(long)]
    pub code: String,
}


impl GetExchangeArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {

        match client.find_exchange(|e| e.code == self.code) {
            Ok((pubkey, exchange)) => {
                println!(
                    "pubkey: {}\r\ncode: {}\r\nname: {}\r\nlat: {}\r\nlng: {}\r\nloc_id: {}\r\nstatus: {}\r\nowner: {}",
                    pubkey, 
                    exchange.code,
                    exchange.name,
                    exchange.lat,
                    exchange.lng,
                    exchange.loc_id,
                    exchange.status,
                    exchange.owner
                );
                    },
            Err(e) => println!("Error: {:?}", e),
        }


        Ok(())
    }
}