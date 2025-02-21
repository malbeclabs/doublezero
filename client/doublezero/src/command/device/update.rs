use std::str::FromStr;
use clap::Args;
use double_zero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use crate::{helpers::print_error, requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON}};

#[derive(Args, Debug)]
pub struct UpdateDeviceArgs {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long)]
    pub code: String,
    #[arg(long)]
    pub public_ip: String,
    #[arg(long)]
    pub dz_prefix: String,
}

impl UpdateDeviceArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = Pubkey::from_str(&self.pubkey)?;

        match client.get_device(&pubkey) {
            Ok(device) => {
                match client.update_device(
                    device.index,
                    &self.code,
                    DeviceType::Switch,
                    ipv4_parse(&self.public_ip),
                    networkv4_parse(&self.dz_prefix),
                ) {
                    Ok(_) => println!("Device updated"),
                    Err(e) => print_error(e),
                }

            },
            Err(_) => println!("Device not found"),
        }

        Ok(())
    }
}