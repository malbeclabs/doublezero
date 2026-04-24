use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::{
    device::get::GetDeviceCommand,
    metro::{get::GetMetroCommand, setdevice::SetDeviceMetroCommand},
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct SetDeviceMetroCliCommand {
    /// Metro Pubkey to update
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Device 1 Pubkey or code to set
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub device1: Option<String>,
    /// Device 2 Pubkey or code to set
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub device2: Option<String>,
}

impl SetDeviceMetroCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, _) = client.get_metro(GetMetroCommand {
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

        let signature = client.setdevice_metro(SetDeviceMetroCommand {
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
        metro::setdevice::SetDeviceMetroCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::{
            device::get::GetDeviceCommand,
            metro::{get::GetMetroCommand, setdevice::SetDeviceMetroCommand},
        },
        get_metro_pda, AccountType, Device, DeviceStatus, DeviceType, Metro, MetroStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_exchange_setdevice() {
        let mut client = create_test_client();

        let (metro_pubkey, _bump_seed) = get_metro_pda(&client.get_program_id(), 1);
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
            facility_pk: Pubkey::new_unique(),
            metro_pk: metro_pubkey,
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
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
        };

        let metro = Metro {
            account_type: AccountType::Metro,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            name: "Test Metro".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 12.34,
            lng: 56.78,
            bgp_community: 1,
            unused: 0,
            status: MetroStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));

        client
            .expect_get_metro()
            .with(predicate::eq(GetMetroCommand {
                pubkey_or_code: metro_pubkey.to_string(),
            }))
            .returning(move |_| Ok((metro_pubkey, metro.clone())));

        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device_pk.to_string(),
            }))
            .returning(move |_| Ok((device_pk, device.clone())));

        client
            .expect_setdevice_metro()
            .with(predicate::eq(SetDeviceMetroCommand {
                pubkey: metro_pubkey,
                device1_pubkey: Some(device_pk),
                device2_pubkey: None,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        // Expected success
        let mut output = Vec::new();
        let res = SetDeviceMetroCliCommand {
            pubkey: metro_pubkey.to_string(),
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
