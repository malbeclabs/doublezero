use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::device::{delete::DeleteDeviceCommand, get::GetDeviceCommand};
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteDeviceCliCommand {
    /// Device Pubkey to delete
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
}

impl DeleteDeviceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, _) = client
            .get_device(GetDeviceCommand {
                pubkey_or_code: self.pubkey,
            })
            .map_err(|_| eyre::eyre!("Device not found"))?;

        let signature = client.delete_device(DeleteDeviceCommand { pubkey })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        device::delete::DeleteDeviceCliCommand,
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_program_common::types::NetworkV4List;
    use doublezero_sdk::{
        commands::{
            device::{delete::DeleteDeviceCommand, get::GetDeviceCommand},
            exchange::get::GetExchangeCommand,
        },
        get_device_pda, AccountType, Device, DeviceStatus, Exchange, ExchangeStatus,
        GetLocationCommand, Location, LocationStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

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

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let location_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let location = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
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
            reference_count: 0,
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 0.0,
            lng: 0.0,
            bgp_community: 0,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: exchange_pk,
        };

        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            contributor_pk,
            location_pk,
            exchange_pk,
            device_type: doublezero_sdk::DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: NetworkV4List::default(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: pda_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_users_count: 0,
            max_unicast_users: 0,
            max_multicast_users: 0,
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
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
            .with(predicate::eq(DeleteDeviceCommand { pubkey: pda_pubkey }))
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
