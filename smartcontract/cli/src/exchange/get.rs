use clap::Args;
use doublezero_sdk::commands::exchange::get::GetExchangeCommand;
use doublezero_sdk::*;

#[derive(Args, Debug)]
pub struct GetExchangeArgs {
    #[arg(long)]
    pub code: String,
}

impl GetExchangeArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        let (pubkey, exchange) = GetExchangeCommand {
            pubkey_or_code: self.code,
        }
        .execute(client)?;

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
