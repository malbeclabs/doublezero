use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::device::delete::DeleteDeviceCommand;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteDeviceCliCommand {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteDeviceCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
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
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}
