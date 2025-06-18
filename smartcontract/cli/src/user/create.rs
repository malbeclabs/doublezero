use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_pubkey,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::{
    commands::{device::get::GetDeviceCommand, user::create::CreateUserCommand},
    *,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateUserCliCommand {
    /// Device Pubkey or code to associate with the user
    #[arg(long)]
    pub device: String,
    /// Client IP address in IPv4 format
    #[arg(long)]
    pub client_ip: String,
    /// Allocate a new address for the user
    #[arg(short, long, default_value_t = false)]
    pub allocate_addr: bool,
}

impl CreateUserCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let device_pk = match parse_pubkey(&self.device) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client
                    .get_device(GetDeviceCommand {
                        pubkey_or_code: self.device.clone(),
                    })
                    .map_err(|_| eyre::eyre!("Device not found"))?;
                pubkey
            }
        };

        let (signature, _pubkey) = client.create_user(CreateUserCommand {
            user_type: if self.allocate_addr {
                UserType::IBRLWithAllocatedIP
            } else {
                UserType::IBRL
            },
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: ipv4_parse(&self.client_ip),
        })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    };
    use doublezero_sdk::{
        commands::{device::get::GetDeviceCommand, user::create::CreateUserCommand},
        AccountType, Device, DeviceStatus, DeviceType, UserCYOA, UserType,
    };
    use doublezero_serviceability::pda::get_user_pda;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    use crate::{tests::utils::create_test_client, user::create::CreateUserCliCommand};

    #[test]
    fn test_cli_user_create() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_user_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let device_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            code: "device1".to_string(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [10, 0, 0, 1],
            dz_prefixes: vec![([10, 0, 0, 1], 24), ([11, 0, 0, 1], 24)],
            owner: device_pubkey,
            status: DeviceStatus::Activated,
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: "device1".to_string(),
            }))
            .returning(move |_| Ok((device_pubkey, device.clone())));

        client
            .expect_create_user()
            .with(predicate::eq(CreateUserCommand {
                user_type: UserType::IBRL,
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [10, 0, 0, 1],
            }))
            .times(1)
            .returning(move |_| Ok((signature, pda_pubkey)));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = CreateUserCliCommand {
            device: "device1".to_string(),
            client_ip: "10.0.0.1".to_string(),
            allocate_addr: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
