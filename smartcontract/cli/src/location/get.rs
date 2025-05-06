use clap::Args;
use doublezero_sdk::commands::location::get::GetLocationCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetLocationArgs {
    #[arg(long)]
    pub code: String,
}

impl GetLocationArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let (pubkey, location) = GetLocationCommand {
            pubkey_or_code: self.code,
        }
        .execute(client)?;

        writeln!(out, 
                "pubkey: {},\r\ncode: {}\r\nname: {}\r\ncountry: {}\r\nlat: {}\r\nlng: {}\r\nloc_id: {}\r\nstatus: {}\r\nowner: {}",
                pubkey,
                location.code,
                location.name,
                location.country,
                location.lat,
                location.lng,
                location.loc_id,
                location.status,
                location.owner
            )?;

        Ok(())
    }
}
