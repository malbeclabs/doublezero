use clap::Args;
use double_zero_sdk::*;

use crate::helpers::print_error;

#[derive(Args, Debug)]
pub struct GetLocationArgs {
    #[arg(long)]
    pub code: String,
}

impl GetLocationArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {

        match client.find_location(|l| l.code == self.code) {
            Ok((pubkey, location)) => println!(
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
            ),
            Err(e) => print_error(e),
        }

        Ok(())
    }
}
