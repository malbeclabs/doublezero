use clap::Args;
use doublezero_sdk::*;
use doublezero_sdk::commands::location::get::GetLocationCommand;

#[derive(Args, Debug)]
pub struct GetLocationArgs {
    #[arg(long)]
    pub code: String,
}

impl GetLocationArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {

        let (pubkey, location) = GetLocationCommand{ pubkey_or_code: self.code }.execute(client)?;

        println!(
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
            );

        Ok(())
    }
}
