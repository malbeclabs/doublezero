use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::*;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use doublezero_sdk::commands::device::delete::DeleteDeviceCommand;

#[derive(Args, Debug)]
pub struct DeleteDeviceArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteDeviceArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, device) = GetDeviceCommand{ pubkey_or_code: self.pubkey }.execute(client)?;
        let _ = DeleteDeviceCommand{ index: device.index }.execute(client)?;
            println!("Device deleted");

        Ok(())
    }
}
