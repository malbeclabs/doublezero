use clap::Args;
use double_zero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;

use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct UpdateLocationArgs {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long)]
    pub code: Option<String>,
    #[arg(long)]
    pub name: Option<String>,
    #[arg(long)]
    pub country: Option<String>,
    #[arg(long, allow_hyphen_values(true))]
    pub lat: Option<f64>,
    #[arg(long, allow_hyphen_values(true))]
    pub lng: Option<f64>,
    #[arg(long)]
    pub loc_id: Option<u32>,
}

impl UpdateLocationArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = Pubkey::from_str(&self.pubkey)?;
        match client.get_location(&pubkey) {
            Ok(location) => {
                client.update_location(
                    location.index,
                    self.code,
                    self.name,
                    self.country,
                    self.lat,
                    self.lng,
                    self.loc_id,
                )?;
                println!("Location updated");
            }
            Err(_) => println!("Location not found"),
        }

        Ok(())
    }
}
