use clap::Args;
use double_zero_sdk::*;
use double_zero_sdk::commands::device::get::GetDeviceCommand;
use double_zero_sdk::commands::device::update::UpdateDeviceCommand;

use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct UpdateDeviceArgs {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long)]
    pub code: Option<String>,
    #[arg(long)]
    pub public_ip: Option<String>,
    #[arg(long)]
    pub dz_prefixes: Option<String>,
}

impl UpdateDeviceArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, device) = GetDeviceCommand{ pubkey_or_code: self.pubkey }.execute(client)?;
        let _ = UpdateDeviceCommand {
            index: device.index,
            code: self.code,
            device_type: Some(DeviceType::Switch),
            public_ip: self.public_ip.map(|ip| ipv4_parse(&ip)),
            dz_prefixes: self.dz_prefixes.map(|ip| networkv4_list_parse(&ip)),
        }.execute(client)?;

        println!("Device updated");

        Ok(())
    }
}
