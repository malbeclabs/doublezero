use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::device::delete::DeleteDeviceCommand;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use doublezero_sdk::*;

#[derive(Args, Debug)]
pub struct DeleteDeviceArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteDeviceArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: self.pubkey,
        }
        .execute(client)
        .map_err(|_| eyre::eyre!("Device not found"))?;

        let signature = DeleteDeviceCommand {
            index: device.index,
        }
        .execute(client)?;
        println!("Signature: {}", signature);

        Ok(())
    }
}
