use crate::{
    helpers::{parse_pubkey, print_error},
    requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use double_zero_sdk::*;
use double_zero_sdk::commands::location::get::GetLocationCommand;
use double_zero_sdk::commands::exchange::get::GetExchangeCommand;


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
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let location_pk = match parse_pubkey(&self.location) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = GetLocationCommand {
                        pubkey_or_code: self.location,
                    }.execute(client)
                    .map_err(|_| eyre::eyre!("Location not found"))?;
                pubkey
            }
        };

        let exchange_pk = match parse_pubkey(&self.exchange) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = GetExchangeCommand {
                    pubkey_or_code: self.exchange,
                }.execute(client)
                    .map_err(|_| eyre::eyre!("Exchange not found"))?;
                pubkey
            }
        };

        match client.create_device(
            &self.code,
            location_pk,
            exchange_pk,
            DeviceType::Switch,
            ipv4_parse(&self.public_ip),
            networkv4_list_parse(&self.dz_prefixes),
        ) {
            Ok((_signature, pda_pubkey)) => println!("{}", pda_pubkey),
            Err(e) => print_error(e),
        };

        Ok(())
    }
}
