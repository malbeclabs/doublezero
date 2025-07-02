use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{
        validate_code, validate_parse_ipv4, validate_parse_networkv4_list, validate_pubkey,
        validate_pubkey_or_code,
    },
};
use clap::Args;
use doublezero_sdk::{
    commands::device::{
        get::GetDeviceCommand, list::ListDeviceCommand, update::UpdateDeviceCommand,
    },
    *,
};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct UpdateDeviceCliCommand {
    /// Device Pubkey to update
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Updated code for the device
    #[arg(long, value_parser = validate_code)]
    pub code: Option<String>,
    /// Updated public IPv4 address for the device (e.g. 10.0.0.1)
    #[arg(long, value_parser = validate_parse_ipv4)]
    pub public_ip: Option<IpV4>,
    /// Updated list of DZ prefixes in comma-separated CIDR format (e.g. 10.1.0.0/16,10.2.0.0/16)
    #[arg(long, value_parser = validate_parse_networkv4_list)]
    pub dz_prefixes: Option<NetworkV4List>,
    /// Metrics publisher Pubkey (optional)
    #[arg(long, value_parser = validate_pubkey)]
    pub metrics_publisher: Option<String>,
}

impl UpdateDeviceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let devices = client.list_device(ListDeviceCommand)?;
        if let Some(code) = &self.code {
            if devices
                .iter()
                .any(|(pk, d)| d.code == *code && pk.to_string() != self.pubkey)
            {
                return Err(eyre::eyre!("Device with code '{}' already exists", code));
            }
        }
        if let Some(public_ip) = &self.public_ip {
            if devices
                .iter()
                .any(|(pk, d)| d.public_ip == *public_ip && pk.to_string() != self.pubkey)
            {
                return Err(eyre::eyre!(
                    "Device with public ip '{}' already exists",
                    ipv4_to_string(public_ip)
                ));
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

        let (pubkey, _) = client.get_device(GetDeviceCommand {
            pubkey_or_code: self.pubkey,
        })?;
        let signature = client.update_device(UpdateDeviceCommand {
            pubkey,
            code: self.code,
            device_type: Some(DeviceType::Switch),
            public_ip: self.public_ip,
            dz_prefixes: self.dz_prefixes,
            metrics_publisher,
        })?;
        writeln!(out, "Signature: {signature}",)?;

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

        let location_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            location_pk,
            exchange_pk,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: pda_pubkey,
        };
        let device2 = Device {
            account_type: AccountType::Device,
            index: 2,
            bump_seed: 254,
            code: "test2".to_string(),
            location_pk,
            exchange_pk,
            device_type: DeviceType::Switch,
            public_ip: [2, 3, 4, 5],
            dz_prefixes: vec![([2, 3, 4, 5], 32)],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: pda_pubkey,
        };
        let device3 = Device {
            account_type: AccountType::Device,
            index: 3,
            bump_seed: 253,
            code: "test3".to_string(),
            location_pk,
            exchange_pk,
            device_type: DeviceType::Switch,
            public_ip: [3, 4, 5, 6],
            dz_prefixes: vec![([3, 4, 5, 6], 32)],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: pda_pubkey,
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
                device_type: Some(DeviceType::Switch),
                public_ip: Some([1, 2, 3, 4]),
                dz_prefixes: Some(vec![([1, 2, 3, 4], 32)]),
                metrics_publisher: Some(Pubkey::from_str_const(
                    "HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx",
                )),
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        // Expected success
        let mut output = Vec::new();
        let res = UpdateDeviceCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("test".to_string()),
            public_ip: Some([1, 2, 3, 4]),
            dz_prefixes: Some(vec![([1, 2, 3, 4], 32)]),
            metrics_publisher: Some("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx".to_string()),
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

        let location_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            location_pk,
            exchange_pk,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: pda_pubkey,
        };
        let device2 = Device {
            account_type: AccountType::Device,
            index: 2,
            bump_seed: 254,
            code: "existing_code".to_string(),
            location_pk,
            exchange_pk,
            device_type: DeviceType::Switch,
            public_ip: [2, 3, 4, 5],
            dz_prefixes: vec![([2, 3, 4, 5], 32)],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: other_pubkey,
        };
        let device_list = HashMap::from([(pda_pubkey, device1.clone()), (other_pubkey, device2)]);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand))
            .returning(move |_| Ok(device_list.clone()));

        // Expected failure - trying to update device1 with code that exists on device2
        let mut output = Vec::new();
        let res = UpdateDeviceCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("existing_code".to_string()),
            public_ip: None,
            dz_prefixes: None,
            metrics_publisher: None,
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

        let location_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            location_pk,
            exchange_pk,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: pda_pubkey,
        };
        let device2 = Device {
            account_type: AccountType::Device,
            index: 2,
            bump_seed: 254,
            code: "test2".to_string(),
            location_pk,
            exchange_pk,
            device_type: DeviceType::Switch,
            public_ip: [10, 20, 30, 40],
            dz_prefixes: vec![([10, 20, 30, 40], 32)],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: other_pubkey,
        };
        let device_list = HashMap::from([(pda_pubkey, device1.clone()), (other_pubkey, device2)]);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand))
            .returning(move |_| Ok(device_list.clone()));

        // Expected failure - trying to update device1 with public IP that exists on device2
        let mut output = Vec::new();
        let res = UpdateDeviceCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: None,
            public_ip: Some([10, 20, 30, 40]),
            dz_prefixes: None,
            metrics_publisher: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("Device with public ip '10.20.30.40' already exists"));
    }
}
