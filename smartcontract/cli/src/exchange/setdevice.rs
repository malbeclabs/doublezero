use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::{
    device::get::GetDeviceCommand,
    exchange::{get::GetExchangeCommand, setdevice::SetDeviceExchangeCommand},
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct SetDeviceExchangeCliCommand {
    /// Exchange Pubkey to update
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Device 1 Pubkey or code to set
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub device1: Option<String>,
    /// Device 2 Pubkey or code to set
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub device2: Option<String>,
}

impl SetDeviceExchangeCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, _) = client.get_exchange(GetExchangeCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let device1_pubkey = self.device1.and_then(|d| {
            client
                .get_device(GetDeviceCommand { pubkey_or_code: d })
                .map(|(pubkey, _device)| pubkey)
                .ok()
        });

        let device2_pubkey = self.device2.and_then(|d| {
            client
                .get_device(GetDeviceCommand { pubkey_or_code: d })
                .map(|(pubkey, _device)| pubkey)
                .ok()
        });

        let signature = client.setdevice_exchange(SetDeviceExchangeCommand {
            pubkey,
            device1_pubkey,
            device2_pubkey,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        exchange::setdevice::SetDeviceExchangeCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::{
            device::get::GetDeviceCommand,
            exchange::{get::GetExchangeCommand, setdevice::SetDeviceExchangeCommand},
        },
        get_exchange_pda, AccountType, Device, DeviceStatus, DeviceType, Exchange, ExchangeStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_exchange_setdevice() {
        let mut client = create_test_client();

        let (exchange_pubkey, _bump_seed) = get_exchange_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let device_pk = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            code: "test_device".to_string(),
            owner: Pubkey::new_unique(),
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: exchange_pubkey,
            metrics_publisher_pk: Pubkey::new_unique(),
            status: DeviceStatus::Activated,
            device_type: DeviceType::Hybrid,
            dz_prefixes: "10.0.0.1/31".parse().unwrap(),
            interfaces: Vec::new(),
            mgmt_vrf: "".to_string(),
            public_ip: "100.0.0.1".parse().unwrap(),
            reference_count: 0,
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

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 12.34,
            lng: 56.78,
            bgp_community: 1,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));

        client
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: exchange_pubkey.to_string(),
            }))
            .returning(move |_| Ok((exchange_pubkey, exchange.clone())));

        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device_pk.to_string(),
            }))
            .returning(move |_| Ok((device_pk, device.clone())));

        client
            .expect_setdevice_exchange()
            .with(predicate::eq(SetDeviceExchangeCommand {
                pubkey: exchange_pubkey,
                device1_pubkey: Some(device_pk),
                device2_pubkey: None,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        // Expected success
        let mut output = Vec::new();
        let res = SetDeviceExchangeCliCommand {
            pubkey: exchange_pubkey.to_string(),
            device1: Some(device_pk.to_string()),
            device2: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
