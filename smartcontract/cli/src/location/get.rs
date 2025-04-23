use clap::Args;
use double_zero_sdk::*;
use double_zero_sdk::commands::location::get::GetLocationCommand;

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
