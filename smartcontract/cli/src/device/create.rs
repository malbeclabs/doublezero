use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_pubkey,
    poll_for_activation::poll_for_device_activated,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{validate_code, validate_pubkey, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_program_common::types::NetworkV4List;
use doublezero_sdk::{
    commands::{
        contributor::get::GetContributorCommand,
        device::{create::CreateDeviceCommand, list::ListDeviceCommand},
        exchange::get::GetExchangeCommand,
        location::get::GetLocationCommand,
    },
    *,
};
use doublezero_serviceability::state::device::DeviceDesiredStatus;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr, str::FromStr};

#[derive(Args, Debug)]
pub struct CreateDeviceCliCommand {
    /// Unique device code
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Contributor (pubkey or code) associated with the device
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub contributor: String,
    /// Device type (hybrid, transit, edge)
    #[arg(long, default_value = "hybrid")]
    pub device_type: String,
    /// Location (pubkey or code) associated with the device
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub location: String,
    /// Exchange (pubkey or code) associated with the device
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub exchange: String,
    /// Device public IPv4 address (e.g. 10.0.0.1)
    #[arg(long)]
    pub public_ip: Ipv4Addr,
    /// List of DZ prefixes in comma-separated CIDR format (e.g. 10.1.0.0/16,10.2.0.0/16)
    #[arg(long)]
    pub dz_prefixes: NetworkV4List,
    /// Metrics publisher public key (optional, defaults to zeroed pubkey)
    #[arg(long, value_parser = validate_pubkey)]
    pub metrics_publisher: Option<String>,
    /// Management VRF name (optional)
    #[arg(long, default_value = "default")]
    pub mgmt_vrf: String,
    /// Desired status for the device (optional)
    #[arg(long)]
    pub desired_status: Option<DeviceDesiredStatus>,
    /// Wait for the device to be activated
    #[arg(short, long, default_value_t = false)]
    pub wait: bool,
}

impl CreateDeviceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let devices = client.list_device(ListDeviceCommand)?;
        if devices.iter().any(|(_, d)| d.code == self.code) {
            eyre::bail!("Device with code '{}' already exists", self.code);
        }
        if devices.iter().any(|(_, d)| d.public_ip == self.public_ip) {
            eyre::bail!("Device with public ip '{}' already exists", &self.public_ip);
        }

        for device in devices.values() {
            for dz_prefix in device.dz_prefixes.iter() {
                if dz_prefix.contains(self.public_ip) {
                    eyre::bail!(
                        "Public IP '{}' conflicts with existing device '{}' dz_prefix '{}'",
                        self.public_ip,
                        device.code,
                        dz_prefix
                    );
                }
            }
        }

        for dz_prefix in self.dz_prefixes.iter() {
            if dz_prefix.contains(self.public_ip) {
                eyre::bail!(
                    "Public IP '{}' conflicts with device's own dz_prefix '{}'",
                    self.public_ip,
                    dz_prefix
                );
            }
        }

        let contributor_pk = match parse_pubkey(&self.contributor) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client
                    .get_contributor(GetContributorCommand {
                        pubkey_or_code: self.contributor.clone(),
                    })
                    .map_err(|_| eyre::eyre!("Contributor not found"))?;
                pubkey
            }
        };

        let location_pk = match parse_pubkey(&self.location) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client
                    .get_location(GetLocationCommand {
                        pubkey_or_code: self.location.clone(),
                    })
                    .map_err(|_| eyre::eyre!("Location not found"))?;
                pubkey
            }
        };

        let exchange_pk = match parse_pubkey(&self.exchange) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client
                    .get_exchange(GetExchangeCommand {
                        pubkey_or_code: self.exchange.clone(),
                    })
                    .map_err(|_| eyre::eyre!("Exchange not found"))?;
                pubkey
            }
        };

        let metrics_publisher = if let Some(metrics_publisher) = &self.metrics_publisher {
            if metrics_publisher == "me" {
                client.get_payer()
            } else {
                match Pubkey::from_str(metrics_publisher) {
                    Ok(pk) => pk,
                    Err(_) => eyre::bail!("Invalid metrics publisher Pubkey"),
                }
            }
        } else {
            client.get_payer()
        };

        let device_type = match DeviceType::from_str(&self.device_type) {
            Ok(dt) => dt,
            Err(e) => return Err(eyre::eyre!(e)),
        };

        let (signature, pubkey) = client.create_device(CreateDeviceCommand {
            code: self.code.clone(),
            contributor_pk,
            location_pk,
            exchange_pk,
            device_type,
            public_ip: self.public_ip,
            dz_prefixes: self.dz_prefixes,
            metrics_publisher,
            mgmt_vrf: self.mgmt_vrf.clone(),
            desired_status: self.desired_status,
        })?;
        writeln!(out, "Signature: {signature}")?;

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
        device::create::CreateDeviceCliCommand,
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::{
            contributor::get::GetContributorCommand,
            device::{create::CreateDeviceCommand, list::ListDeviceCommand},
            exchange::get::GetExchangeCommand,
        },
        get_device_pda, AccountType, Contributor, ContributorStatus, DeviceType, Exchange,
        ExchangeStatus, GetLocationCommand, Location, LocationStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_device_create() {
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

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let contributor = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            status: ContributorStatus::Activated,
            owner: contributor_pk,
            ops_manager_pk: Pubkey::default(),
        };

        client
            .expect_get_contributor()
            .with(predicate::eq(GetContributorCommand {
                pubkey_or_code: contributor_pk.to_string(),
            }))
            .returning(move |_| Ok((contributor_pk, contributor.clone())));

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
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand))
            .returning(move |_| Ok(HashMap::new()));
        client
            .expect_create_device()
            .with(predicate::eq(CreateDeviceCommand {
                code: "test".to_string(),
                contributor_pk,
                location_pk,
                exchange_pk,
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 1].into(),
                dz_prefixes: "10.1.0.0/16".parse().unwrap(),
                metrics_publisher: Pubkey::default(),
                mgmt_vrf: "default".to_string(),
                desired_status: None,
            }))
            .returning(move |_| Ok((signature, pda_pubkey)));

        let mut output = Vec::new();
        let res = CreateDeviceCliCommand {
            code: "test".to_string(),
            contributor: contributor_pk.to_string(),
            location: location_pk.to_string(),
            exchange: exchange_pk.to_string(),
            device_type: "hybrid".to_string(),
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "10.1.0.0/16".parse().unwrap(),
            metrics_publisher: Some(Pubkey::default().to_string()),
            mgmt_vrf: "default".to_string(),
            desired_status: None,
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_device_create_fails_when_public_ip_conflicts_with_existing_device_dz_prefix() {
        use doublezero_sdk::{Device, DeviceStatus};

        let mut client = create_test_client();

        let location_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");

        // Create an existing device with dz_prefix that will conflict
        let existing_device_pk =
            Pubkey::from_str_const("HQ4UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let existing_device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "existing-device".to_string(),
            contributor_pk,
            location_pk,
            exchange_pk,
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            // This dz_prefix includes 10.1.5.10
            dz_prefixes: "10.1.0.0/16".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            status: DeviceStatus::Activated,
            mgmt_vrf: String::default(),
            interfaces: vec![],
            users_count: 0,
            max_users: 100,
            owner: Pubkey::default(),
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };

        let mut devices = HashMap::new();
        devices.insert(existing_device_pk, existing_device);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand))
            .returning(move |_| Ok(devices.clone()));

        let mut output = Vec::new();
        // Create a device with public_ip 10.1.5.10, which is within existing device's dz_prefix
        let res = CreateDeviceCliCommand {
            code: "new-device".to_string(),
            contributor: contributor_pk.to_string(),
            location: location_pk.to_string(),
            exchange: exchange_pk.to_string(),
            device_type: "hybrid".to_string(),
            public_ip: [10, 1, 5, 10].into(), // This is within 10.1.0.0/16
            dz_prefixes: "192.168.0.0/16".parse().unwrap(),
            metrics_publisher: Some(Pubkey::default().to_string()),
            mgmt_vrf: String::default(),
            desired_status: None,
            wait: false,
        }
        .execute(&client, &mut output);

        assert!(res.is_err());
        let err = res.unwrap_err();
        assert!(err.to_string().contains("Public IP '10.1.5.10' conflicts with existing device 'existing-device' dz_prefix '10.1.0.0/16'"));
    }

    #[test]
    fn test_cli_device_create_fails_when_public_ip_conflicts_with_own_dz_prefix() {
        let mut client = create_test_client();

        let location_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand))
            .returning(move |_| Ok(HashMap::new()));

        let mut output = Vec::new();
        // Create a device where public_ip is within its own dz_prefix
        let res = CreateDeviceCliCommand {
            code: "test-device".to_string(),
            contributor: contributor_pk.to_string(),
            location: location_pk.to_string(),
            exchange: exchange_pk.to_string(),
            device_type: "hybrid".to_string(),
            public_ip: [10, 1, 5, 10].into(), // This is within 10.1.0.0/16
            dz_prefixes: "10.1.0.0/16".parse().unwrap(), // Own prefix contains public_ip
            metrics_publisher: Some(Pubkey::default().to_string()),
            mgmt_vrf: String::default(),
            desired_status: None,
            wait: false,
        }
        .execute(&client, &mut output);

        assert!(res.is_err());
        let err = res.unwrap_err();
        assert!(err
            .to_string()
            .contains("Public IP '10.1.5.10' conflicts with device's own dz_prefix '10.1.0.0/16'"));
    }
}
