use clap::Args;
use doublezero_sdk::commands::exchange::get::GetExchangeCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetExchangeArgs {
    #[arg(long)]
    pub code: String,
}

impl GetExchangeArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let (pubkey, exchange) = GetExchangeCommand {
            pubkey_or_code: self.code,
        }
        .execute(client)?;

        writeln!(out, 
                "pubkey: {},\r\ncode: {}\r\nname: {}\r\nlat: {}\r\nlng: {}\r\nloc_id: {}\r\nstatus: {}\r\nowner: {}",
                pubkey,
                exchange.code,
                exchange.name,
                exchange.lat,
                exchange.lng,
                exchange.loc_id,
                exchange.status,
                exchange.owner
            )?;

        Ok(())
    }
}
