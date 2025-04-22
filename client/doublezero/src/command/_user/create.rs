use clap::Args;
use double_zero_sdk::*;
use crate::{helpers::parse_pubkey, requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON}};

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
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let device_pk = match parse_pubkey(&self.device) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client.find_device(|d| d.code == self.device)
                    .map_err(|_| eyre::eyre!("Device not found"))?;
                pubkey
            }
        };

        match client.create_user(
            if self.allocate_addr {
                UserType::IBRLWithAllocatedIP
            } else {
                UserType::IBRL
            },
            device_pk, 
            UserCYOA::GREOverDIA,
            ipv4_parse(&self.client_ip), 
        ) {
            Ok((_, pubkey)) => println!("{}", pubkey),
            Err(e) => eprintln!("Error: {:?}", e),

        }

        Ok(())
    }
}
