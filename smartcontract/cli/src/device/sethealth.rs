use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::device::{get::GetDeviceCommand, sethealth::SetDeviceHealthCommand};
use doublezero_serviceability::state::device::DeviceHealth;
use std::io::Write;

#[derive(Args, Debug)]
pub struct SetDeviceHealthCliCommand {
    /// Device Pubkey to update
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,

    /// Device Health to set (pending, ready-for-links, ready-for-users, impaired)
    #[arg(long)]
    pub health: DeviceHealth,
}

impl SetDeviceHealthCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, _) = client.get_device(GetDeviceCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let signature = client.set_device_health(SetDeviceHealthCommand {
            pubkey,
            health: self.health,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{
        device::sethealth::SetDeviceHealthCliCommand,
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::device::{
            get::GetDeviceCommand, list::ListDeviceCommand, sethealth::SetDeviceHealthCommand,
        },
        get_device_pda, AccountType, Device, DeviceStatus, DeviceType,
    };
    use doublezero_serviceability::state::device::DeviceHealth;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_device_set_health_success() {
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
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            contributor_pk,
            location_pk,
            exchange_pk,
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "10.1.2.3/32".parse().unwrap(),
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
            reserved_seats: 0,
        };
        let device2 = Device {
            account_type: AccountType::Device,
            index: 2,
            bump_seed: 254,
            reference_count: 0,
            code: "test2".to_string(),
            contributor_pk,
            location_pk,
            exchange_pk,
            device_type: DeviceType::Hybrid,
            public_ip: [2, 3, 4, 5].into(),
            dz_prefixes: "2.3.4.5/32".parse().unwrap(),
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
            reserved_seats: 0,
        };
        let device3 = Device {
            account_type: AccountType::Device,
            index: 3,
            bump_seed: 253,
            reference_count: 0,
            code: "test3".to_string(),
            contributor_pk,
            location_pk,
            exchange_pk,
            device_type: DeviceType::Hybrid,
            public_ip: [3, 4, 5, 6].into(),
            dz_prefixes: "3.4.5.6/32".parse().unwrap(),
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
            reserved_seats: 0,
        };
        let device_list = HashMap::from([
            (pda_pubkey, device1.clone()),
            (Pubkey::new_unique(), device2),
            (Pubkey::new_unique(), device3),
        ]);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, device1.clone())));
        client
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand))
            .returning(move |_| Ok(device_list.clone()));

        client
            .expect_set_device_health()
            .with(predicate::eq(SetDeviceHealthCommand {
                pubkey: pda_pubkey,
                health: DeviceHealth::ReadyForUsers,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        // Expected success
        let mut output = Vec::new();
        let res = SetDeviceHealthCliCommand {
            pubkey: pda_pubkey.to_string(),
            health: DeviceHealth::ReadyForUsers,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "{}", res.err().unwrap());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
