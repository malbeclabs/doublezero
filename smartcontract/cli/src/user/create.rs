use crate::helpers::parse_pubkey;
use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use doublezero_sdk::commands::user::create::CreateUserCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateUserCliCommand {
    #[arg(long)]
    pub device: String,
    #[arg(long)]
    pub client_ip: String,
    #[arg(short, long, default_value_t = false)]
    pub allocate_addr: bool,
}

impl CreateUserCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let device_pk = match parse_pubkey(&self.device) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = GetDeviceCommand {
                    pubkey_or_code: self.device.clone(),
                }
                .execute(client)
                .map_err(|_| eyre::eyre!("Device not found"))?;
                pubkey
            }
        };

        let (signature, _pubkey) = CreateUserCommand {
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
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}
