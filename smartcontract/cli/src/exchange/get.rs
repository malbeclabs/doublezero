use clap::Args;
use double_zero_sdk::*;
use double_zero_sdk::commands::exchange::get::GetExchangeCommand;

#[derive(Args, Debug)]
pub struct GetExchangeArgs {
    #[arg(long)]
    pub code: String,
}

impl GetExchangeArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {

        let (pubkey, exchange) = GetExchangeCommand{ pubkey_or_code: self.code }.execute(client)?;

        println!(
                "pubkey: {},\r\ncode: {}\r\nname: {}\r\nlat: {}\r\nlng: {}\r\nloc_id: {}\r\nstatus: {}\r\nowner: {}",
                pubkey,
                exchange.code,
                exchange.name,
                exchange.lat,
                exchange.lng,
                exchange.loc_id,
                exchange.status,
                exchange.owner
            );

        Ok(())
    }
}
