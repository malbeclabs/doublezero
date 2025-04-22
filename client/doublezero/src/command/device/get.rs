use clap::Args;
use double_zero_sdk::*;
use double_zero_sdk::commands::device::get::GetDeviceCommand;

#[derive(Args, Debug)]
pub struct GetDeviceArgs {
    #[arg(long)]
    pub code: String,
}

impl GetDeviceArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {

        let (pubkey, device) = GetDeviceCommand{ pubkey_or_code: self.code }.execute(client)?;

        println!(
            "pubkey: {}\r\ncode: {}\r\nlocation: {}\r\nexchange: {}\r\ndevice_type: {}\r\npublic_ip: {}\r\ndz_prefixes: {}\r\nstatus: {}\r\nowner: {}",
            pubkey,
            device.code,
            device.location_pk,
            device.exchange_pk,
            device.device_type,
            ipv4_to_string(&device.public_ip),
            networkv4_list_to_string(&device.dz_prefixes),
            device.status,
            device.owner
            );

        Ok(())
    }
}
