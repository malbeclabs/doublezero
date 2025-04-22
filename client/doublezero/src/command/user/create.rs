use clap::Args;
use crate::helpers::{parse_pubkey};
use double_zero_sdk::*;
use double_zero_sdk::commands::device::get::GetDeviceCommand;
use double_zero_sdk::commands::user::create::CreateUserCommand;

use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct CreateUserArgs {
    #[arg(long)]
    pub device: String,
    #[arg(long)]
    pub client_ip: String,
    #[arg(short, long, default_value_t = false)]
    pub allocate_addr: bool,
}

impl CreateUserArgs {
     pub async fn execute(&self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let device_pk = match parse_pubkey(&self.device) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = GetDeviceCommand { pubkey_or_code: self.device.clone() }
                .execute(client)
                    .map_err(|_| eyre::eyre!("Device not found"))?;
                pubkey
            }
        };

        let (_signature, pubkey) = CreateUserCommand {
            user_type: if self.allocate_addr {
                UserType::IBRLWithAllocatedIP
            } else {
                UserType::IBRL
            },
            device_pk, 
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: ipv4_parse(&self.client_ip), 
        }
        .execute(client)?;

        println!("{}", pubkey);

        Ok(())
    }
}
