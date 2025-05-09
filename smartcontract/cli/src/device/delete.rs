use crate::doublezerocommand::CliCommand;
use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::device::delete::DeleteDeviceCommand;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteDeviceCliCommand {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteDeviceCliCommand {
    pub fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, device) = client
            .get_device(GetDeviceCommand {
                pubkey_or_code: self.pubkey,
            })
            .map_err(|_| eyre::eyre!("Device not found"))?;

        let signature = client.delete_device(DeleteDeviceCommand {
            index: device.index,
        })?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::device::delete::DeleteDeviceCliCommand;
    use crate::doublezerocommand::CliCommand;
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::commands::device::delete::DeleteDeviceCommand;
    use doublezero_sdk::commands::device::get::GetDeviceCommand;
    use doublezero_sdk::commands::exchange::get::GetExchangeCommand;
    use doublezero_sdk::get_device_pda;
    use doublezero_sdk::AccountType;
    use doublezero_sdk::Device;
    use doublezero_sdk::DeviceStatus;
    use doublezero_sdk::Exchange;
    use doublezero_sdk::ExchangeStatus;
    use doublezero_sdk::GetLocationCommand;
    use doublezero_sdk::Location;
    use doublezero_sdk::LocationStatus;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_device_delete() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_device_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let location_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let location = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            name: "Test Location".to_string(),
            country: "Test Country".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
            status: LocationStatus::Activated,
            owner: location_pk,
        };
        let exchange_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let exchange = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
            status: ExchangeStatus::Activated,
            owner: exchange_pk,
        };

        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            location_pk,
            exchange_pk,
            device_type: doublezero_sdk::DeviceType::Switch,
            public_ip: [10, 0, 0, 1],
            dz_prefixes: vec![],
            status: DeviceStatus::Activated,
            owner: pda_pubkey,
        };

        client
            .expect_get_location()
            .with(predicate::eq(GetLocationCommand {
                pubkey_or_code: location_pk.to_string(),
            }))
            .returning(move |_| Ok((location_pk, location.clone())));
        client
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: exchange_pk.to_string(),
            }))
            .returning(move |_| Ok((exchange_pk, exchange.clone())));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, device.clone())));

        client
            .expect_delete_device()
            .with(predicate::eq(DeleteDeviceCommand { index: 1 }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = DeleteDeviceCliCommand {
            pubkey: pda_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
