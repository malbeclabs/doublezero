
use clap::Args;
use double_zero_sdk::*;

#[derive(Args, Debug)]
pub struct GetDeviceArgs {
    #[arg(long)]
    pub code: String,
}
impl GetDeviceArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        
        match client.find_device(|d| d.code == self.code) {
            Ok((pubkey, device)) => {
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
            },
            Err(e) => println!("Error: {:?}", e),
        }
        Ok(())
    }
}

