use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::exchange::get::GetExchangeCommand;
use doublezero_sdk::commands::exchange::update::UpdateExchangeCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateExchangeArgs {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long)]
    pub code: Option<String>,
    #[arg(long)]
    pub name: Option<String>,
    #[arg(long, allow_hyphen_values(true))]
    pub lat: Option<f64>,
    #[arg(long, allow_hyphen_values(true))]
    pub lng: Option<f64>,
    #[arg(long)]
    pub loc_id: Option<u32>,
}

impl UpdateExchangeArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, exchange) = GetExchangeCommand {
            pubkey_or_code: self.pubkey,
        }
        .execute(client)?;
        let signature = UpdateExchangeCommand {
            index: exchange.index,
            code: self.code,
            name: self.name,
            lat: self.lat,
            lng: self.lng,
            loc_id: self.loc_id,
        }
        .execute(client)?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}
