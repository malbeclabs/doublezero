use clap::Args;
use doublezero_sdk::commands::location::get::GetLocationCommand;
use doublezero_sdk::commands::location::update::UpdateLocationCommand;
use doublezero_sdk::*;

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
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, location) = GetLocationCommand {
            pubkey_or_code: self.pubkey,
        }
        .execute(client)?;
        let _ = UpdateLocationCommand {
            index: location.index,
            code: self.code,
            name: self.name,
            country: self.country,
            lat: self.lat,
            lng: self.lng,
            loc_id: self.loc_id,
        }
        .execute(client)?;

        println!("Location updated");

        Ok(())
    }
}
