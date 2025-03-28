use clap::Args;
use double_zero_sdk::*;

use double_zero_sdk::cli::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct CreateExchangeArgs {
    #[arg(long)]
    pub code: String,
    #[arg(long)]
    pub name: String,
    #[arg(long, allow_hyphen_values(true))]
    pub lat: Option<f64>,
    #[arg(long, allow_hyphen_values(true))]
    pub lng: Option<f64>,
    #[arg(long)]
    pub loc_id: Option<u32>,
}

impl CreateExchangeArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        match client.create_exchange(
            &self.code,
            &self.name,
            self.lat.unwrap_or(0.0),
            self.lng.unwrap_or(0.0),
            self.loc_id.unwrap_or(0),
        ) {
            Ok((_, pubkey)) => println!("{}", pubkey),
            Err(e) => eprintln!("Error: {}", e),
        }

        Ok(())
    }
}
