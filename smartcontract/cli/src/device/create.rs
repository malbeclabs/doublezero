use crate::helpers::parse_pubkey;
use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::device::create::CreateDeviceCommand;
use doublezero_sdk::commands::exchange::get::GetExchangeCommand;
use doublezero_sdk::commands::location::get::GetLocationCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateDeviceArgs {
    #[arg(long)]
    pub code: String,
    #[arg(long)]
    pub location: String,
    #[arg(long)]
    pub exchange: String,
    #[arg(long)]
    pub public_ip: String,
    #[arg(long)]
    pub dz_prefixes: String,
}

impl CreateDeviceArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let location_pk = match parse_pubkey(&self.location) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = GetLocationCommand {
                    pubkey_or_code: self.location.clone(),
                }
                .execute(client)
                .map_err(|_| eyre::eyre!("Location not found"))?;
                pubkey
            }
        };

        let exchange_pk = match parse_pubkey(&self.exchange) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = GetExchangeCommand {
                    pubkey_or_code: self.exchange.clone(),
                }
                .execute(client)
                .map_err(|_| eyre::eyre!("Exchange not found"))?;
                pubkey
            }
        };

        let (signature, _pubkey) = CreateDeviceCommand {
            code: self.code.clone(),
            location_pk,
            exchange_pk,
            device_type: DeviceType::Switch,
            public_ip: ipv4_parse(&self.public_ip),
            dz_prefixes: networkv4_list_parse(&self.dz_prefixes),
        }
        .execute(client)?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}
