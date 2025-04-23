use clap::Args;
use doublezero_sdk::*;

use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct CreateLocationArgs {
    #[arg(long)]
    pub code: String,
    #[arg(long)]
    pub name: String,
    #[arg(long)]
    pub country: String,
    #[arg(long, allow_hyphen_values(true))]
    pub lat: f64,
    #[arg(long, allow_hyphen_values(true))]
    pub lng: f64,
    #[arg(long)]
    pub loc_id: Option<u32>,
}

impl CreateLocationArgs {
    pub async fn execute(&self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;
        
        let (_signature, pubkey) = CreateLocationCommand {
            code: self.code.clone(),
            name: self.name.clone(),
            country: self.country.clone(),
            lat: self.lat,
            lng: self.lng,
            loc_id: self.loc_id,
        }
        .execute(client)?;

        println!("{}", pubkey);

        Ok(())
    }
}
