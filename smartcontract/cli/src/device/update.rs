use crate::doublezerocommand::CliCommand;
use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use doublezero_sdk::commands::device::update::UpdateDeviceCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateDeviceCliCommand {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long)]
    pub code: Option<String>,
    #[arg(long)]
    pub public_ip: Option<String>,
    #[arg(long)]
    pub dz_prefixes: Option<String>,
}

impl UpdateDeviceCliCommand {
    pub fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, device) = client.get_device(GetDeviceCommand {
            pubkey_or_code: self.pubkey,
        })?;
        let signature = client.update_device(UpdateDeviceCommand {
            index: device.index,
            code: self.code,
            device_type: Some(DeviceType::Switch),
            public_ip: self.public_ip.map(|ip| ipv4_parse(&ip)),
            dz_prefixes: self.dz_prefixes.map(|ip| networkv4_list_parse(&ip)),
        })?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::device::update::UpdateDeviceCliCommand;
    use crate::doublezerocommand::CliCommand;
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::commands::device::get::GetDeviceCommand;
    use doublezero_sdk::commands::device::update::UpdateDeviceCommand;
    use doublezero_sdk::get_device_pda;
    use doublezero_sdk::AccountType;
    use doublezero_sdk::Device;
    use doublezero_sdk::DeviceStatus;
    use doublezero_sdk::DeviceType;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_device_update() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_device_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let location_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            location_pk: location_pk,
            exchange_pk: exchange_pk,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            owner: pda_pubkey,
        };

        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, device1.clone())));

        client
            .expect_update_device()
            .with(predicate::eq(UpdateDeviceCommand {
                index: 1,
                code: Some("test".to_string()),
                device_type: Some(DeviceType::Switch),
                public_ip: Some([1, 2, 3, 4]),
                dz_prefixes: Some(vec![([1, 2, 3, 4], 32)]),
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        // Expected success
        let mut output = Vec::new();
        let res = UpdateDeviceCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("test".to_string()),
            public_ip: Some("1.2.3.4".to_string()),
            dz_prefixes: Some("1.2.3.4/32".to_string()),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
