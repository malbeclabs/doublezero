use crate::{
    doublezerocommand::CliCommand,
    poll_for_activation::poll_for_device_activated,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{validate_code, validate_pubkey, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_program_common::types::NetworkV4List;
use doublezero_sdk::{
    commands::device::{
        get::GetDeviceCommand, list::ListDeviceCommand, update::UpdateDeviceCommand,
    },
    *,
};
use doublezero_serviceability::state::device::DeviceDesiredStatus;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr, str::FromStr};

#[derive(Args, Debug)]
pub struct UpdateDeviceCliCommand {
    /// Device Pubkey to update
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Updated code for the device
    #[arg(long, value_parser = validate_code)]
    pub code: Option<String>,
    /// Updated public IPv4 address for the device (e.g. 10.0.0.1)
    #[arg(long)]
    pub public_ip: Option<Ipv4Addr>,
    /// Updated list of DZ prefixes in comma-separated CIDR format (e.g. 10.1.0.0/16,10.2.0.0/16)
    #[arg(long)]
    pub dz_prefixes: Option<NetworkV4List>,
    /// Metrics publisher Pubkey (optional)
    #[arg(long, value_parser = validate_pubkey)]
    pub metrics_publisher: Option<String>,
    /// Contributor Pubkey (optional)
    #[arg(long, value_parser = validate_pubkey)]
    pub contributor: Option<String>,
    /// Location Pubkey (optional)
    #[arg(long, value_parser = validate_pubkey)]
    pub location: Option<String>,
    /// Management VRF name (optional)
    #[arg(long)]
    pub mgmt_vrf: Option<String>,
    /// Maximum number of users for the device (optional)
    #[arg(long)]
    pub max_users: Option<u16>,
    /// Number of users connected to the device (optional)
    #[arg(long)]
    pub users_count: Option<u16>,
    /// Updated status for the device (optional)
    #[arg(long)]
    pub status: Option<DeviceStatus>,
    /// Device type (optional)
    #[arg(long)]
    pub device_type: Option<DeviceType>,
    /// Desired status for the device (optional)
    #[arg(long, hide = true)]
    pub desired_status: Option<DeviceDesiredStatus>,
    /// Reference count for the device (optional, foundation only)
    #[arg(long)]
    pub reference_count: Option<u32>,
    /// Maximum number of unicast users for the device (optional)
    #[arg(long)]
    pub max_unicast_users: Option<u16>,
    /// Maximum number of multicast users for the device (optional)
    #[arg(long)]
    pub max_multicast_users: Option<u16>,
    /// Wait for the device to be activated
    #[arg(short, long, default_value_t = false)]
    pub wait: bool,
}

impl UpdateDeviceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, _) = client.get_device(GetDeviceCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let devices = client.list_device(ListDeviceCommand)?;
        if let Some(code) = &self.code {
            if devices
                .iter()
                .any(|(pk, d)| d.code == *code && *pk != pubkey)
            {
                return Err(eyre::eyre!("Device with code '{}' already exists", code));
            }
        }
        if let Some(public_ip) = &self.public_ip {
            if devices
                .iter()
                .any(|(pk, d)| d.public_ip == *public_ip && *pk != pubkey)
            {
                return Err(eyre::eyre!(
                    "Device with public ip '{}' already exists",
                    public_ip
                ));
            }
        }

        // Check if updated public_ip conflicts with dz_prefixes
        if let Some(new_public_ip) = &self.public_ip {
            let own_dz_prefixes = self
                .dz_prefixes
                .as_ref()
                .or_else(|| devices.get(&pubkey).map(|d| &d.dz_prefixes))
                .unwrap();

            for dz_prefix in own_dz_prefixes.iter() {
                if dz_prefix.contains(*new_public_ip) {
                    eyre::bail!(
                        "Public IP '{}' conflicts with device's own dz_prefix '{}'",
                        new_public_ip,
                        dz_prefix
                    );
                }
            }

            for (pk, device) in devices.iter() {
                if *pk != pubkey {
                    for dz_prefix in device.dz_prefixes.iter() {
                        if dz_prefix.contains(*new_public_ip) {
                            eyre::bail!(
                                "Public IP '{}' conflicts with existing device '{}' dz_prefix '{}'",
                                new_public_ip,
                                device.code,
                                dz_prefix
                            );
                        }
                    }
                }
            }
        }

        let metrics_publisher = if let Some(metrics_publisher) = &self.metrics_publisher {
            if metrics_publisher == "me" {
                Some(client.get_payer())
            } else {
                match Pubkey::from_str(metrics_publisher) {
                    Ok(pk) => Some(pk),
                    Err(_) => return Err(eyre::eyre!("Invalid metrics publisher Pubkey")),
                }
            }
        } else {
            None
        };

        let contributor = if let Some(contributor) = &self.contributor {
            if contributor == "me" {
                Some(client.get_payer())
            } else {
                match Pubkey::from_str(contributor) {
                    Ok(pk) => Some(pk),
                    Err(_) => return Err(eyre::eyre!("Invalid contributor Pubkey")),
                }
            }
        } else {
            None
        };

        let signature = client.update_device(UpdateDeviceCommand {
            pubkey,
            code: self.code,
            device_type: self.device_type,
            public_ip: self.public_ip,
            dz_prefixes: self.dz_prefixes,
            metrics_publisher,
            contributor_pk: contributor,
            location_pk: match &self.location {
                Some(location) => Some(Pubkey::from_str(location)?),
                None => None,
            },
            mgmt_vrf: self.mgmt_vrf,
            interfaces: None,
            max_users: self.max_users,
            users_count: self.users_count,
            status: self.status,
            desired_status: self.desired_status,
            reference_count: self.reference_count,
            max_unicast_users: self.max_unicast_users,
            max_multicast_users: self.max_multicast_users,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        if self.wait {
            let device = poll_for_device_activated(client, &pubkey)?;
            writeln!(out, "Status: {0}", device.status)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{
        device::update::UpdateDeviceCliCommand,
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::device::{
            get::GetDeviceCommand, list::ListDeviceCommand, update::UpdateDeviceCommand,
        },
        get_device_pda, AccountType, Device, DeviceStatus, DeviceType,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_device_update_success() {
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
            .expect_update_device()
            .with(predicate::eq(UpdateDeviceCommand {
                pubkey: pda_pubkey,
                code: Some("test".to_string()),
                device_type: Some(DeviceType::Hybrid),
                public_ip: Some([1, 2, 3, 4].into()),
                dz_prefixes: Some("10.1.2.3/32".parse().unwrap()),
                metrics_publisher: Some(Pubkey::from_str_const(
                    "HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx",
                )),
                contributor_pk: Some(Pubkey::from_str_const(
                    "HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx",
                )),
                location_pk: Some(Pubkey::from_str_const(
                    "HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx",
                )),
                mgmt_vrf: Some("default".to_string()),
                interfaces: None,
                max_users: Some(1025),
                users_count: Some(0),
                status: None,
                desired_status: None,
                reference_count: None,
                max_unicast_users: None,
                max_multicast_users: None,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        // Expected success
        let mut output = Vec::new();
        let res = UpdateDeviceCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("test".to_string()),
            public_ip: Some([1, 2, 3, 4].into()),
            device_type: Some(DeviceType::Hybrid),
            dz_prefixes: Some("10.1.2.3/32".parse().unwrap()),
            metrics_publisher: Some("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx".to_string()),
            contributor: Some("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx".to_string()),
            location: Some("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx".to_string()),
            mgmt_vrf: Some("default".to_string()),
            max_users: Some(1025),
            users_count: Some(0),
            status: None,
            desired_status: None,
            reference_count: None,
            max_unicast_users: None,
            max_multicast_users: None,
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "{}", res.err().unwrap());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_device_update_fails_when_code_exists() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_device_pda(&client.get_program_id(), 1);
        let other_pubkey = Pubkey::new_unique();

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
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
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
            code: "existing_code".to_string(),
            contributor_pk,
            location_pk,
            exchange_pk,
            device_type: DeviceType::Hybrid,
            public_ip: [2, 3, 4, 5].into(),
            dz_prefixes: "2.3.4.5/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: other_pubkey,
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
        let device_list = HashMap::from([(pda_pubkey, device1.clone()), (other_pubkey, device2)]);

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

        // Expected failure - trying to update device1 with code that exists on device2
        let mut output = Vec::new();
        let res = UpdateDeviceCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("existing_code".to_string()),
            device_type: None,
            public_ip: None,
            dz_prefixes: None,
            metrics_publisher: None,
            location: None,
            contributor: None,
            mgmt_vrf: None,
            max_users: Some(255),
            users_count: Some(0),
            status: None,
            desired_status: None,
            reference_count: None,
            max_unicast_users: None,
            max_multicast_users: None,
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("Device with code 'existing_code' already exists"));
    }

    #[test]
    fn test_cli_device_update_fails_when_public_ip_exists() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_device_pda(&client.get_program_id(), 1);
        let other_pubkey = Pubkey::new_unique();

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
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: pda_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 1024,
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
            public_ip: [10, 20, 30, 40].into(),
            dz_prefixes: "10.20.30.40/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: other_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 1024,
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
        let device_list = HashMap::from([(pda_pubkey, device1.clone()), (other_pubkey, device2)]);

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

        // Expected failure - trying to update device1 with public IP that exists on device2
        let mut output = Vec::new();
        let res = UpdateDeviceCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: None,
            public_ip: Some([10, 20, 30, 40].into()),
            dz_prefixes: None,
            metrics_publisher: None,
            device_type: None,
            location: None,
            contributor: None,
            mgmt_vrf: None,
            max_users: None,
            users_count: None,
            status: None,
            desired_status: None,
            reference_count: None,
            max_unicast_users: None,
            max_multicast_users: None,
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("Device with public ip '10.20.30.40' already exists"));
    }
}
