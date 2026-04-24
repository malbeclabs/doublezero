use crate::{doublezerocommand::CliCommand, validators::validate_code};
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::commands::{device::list::ListDeviceCommand, metro::get::GetMetroCommand};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetMetroCliCommand {
    /// Metro Pubkey or code to get details for
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct ExchangeDisplay {
    pub account: String,
    pub code: String,
    pub name: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub device1_pk: Pubkey,
    pub device1: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub device2_pk: Pubkey,
    pub device2: String,
    pub lat: f64,
    pub lng: f64,
    pub bgp_community: u16,
    pub reference_count: u32,
    pub status: String,
    pub owner: String,
}

impl GetMetroCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, metro) = client.get_metro(GetMetroCommand {
            pubkey_or_code: self.code,
        })?;

        let devices = client.list_device(ListDeviceCommand)?;

        let device1 = if metro.device1_pk == Pubkey::default() {
            "(none)".to_string()
        } else {
            devices
                .get(&metro.device1_pk)
                .map_or_else(|| metro.device1_pk.to_string(), |d| d.code.clone())
        };
        let device2 = if metro.device2_pk == Pubkey::default() {
            "(none)".to_string()
        } else {
            devices
                .get(&metro.device2_pk)
                .map_or_else(|| metro.device2_pk.to_string(), |d| d.code.clone())
        };

        let display = ExchangeDisplay {
            account: pubkey.to_string(),
            code: metro.code,
            name: metro.name,
            device1_pk: metro.device1_pk,
            device1,
            device2_pk: metro.device2_pk,
            device2,
            lat: metro.lat,
            lng: metro.lng,
            bgp_community: metro.bgp_community,
            reference_count: metro.reference_count,
            status: metro.status.to_string(),
            owner: metro.owner.to_string(),
        };

        if self.json {
            let json = serde_json::to_string_pretty(&display)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = ExchangeDisplay::headers();
            let fields = display.fields();
            let max_len = headers.iter().map(|h| h.len()).max().unwrap_or(0);
            for (header, value) in headers.iter().zip(fields.iter()) {
                writeln!(out, " {header:<max_len$} | {value}")?;
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{metro::get::GetMetroCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{
        commands::{device::list::ListDeviceCommand, metro::get::GetMetroCommand},
        AccountType, Device, DeviceStatus, DeviceType, Metro, MetroStatus,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, str::FromStr};

    #[test]
    fn test_cli_exchange_get() {
        let mut client = create_test_client();

        let contributor_pubkey =
            Pubkey::from_str("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx").unwrap();
        let facility_pubkey =
            Pubkey::from_str("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo").unwrap();
        let exchange1_pubkey =
            Pubkey::from_str("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB").unwrap();

        let device1_pubkey = Pubkey::from_str("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo").unwrap();
        let device1 = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            reference_count: 0,
            contributor_pk: contributor_pubkey,
            facility_pk: facility_pubkey,
            metro_pk: exchange1_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [192, 168, 1, 1].into(),
            status: DeviceStatus::Pending,
            code: "TestDevice".to_string(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "default".to_string(),
            interfaces: Vec::new(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
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

        client
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand {}))
            .returning(move |_| {
                let mut devices = HashMap::new();
                devices.insert(device1_pubkey, device1.clone());
                Ok(devices)
            });

        let exchange1 = Metro {
            account_type: AccountType::Metro,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            name: "Test Metro".to_string(),
            device1_pk: device1_pubkey,
            device2_pk: Pubkey::default(),
            lat: 12.34,
            lng: 56.78,
            bgp_community: 1,
            unused: 0,
            status: MetroStatus::Activated,
            owner: exchange1_pubkey,
        };

        let exchange2 = exchange1.clone();
        client
            .expect_get_metro()
            .with(predicate::eq(GetMetroCommand {
                pubkey_or_code: exchange1_pubkey.to_string(),
            }))
            .returning(move |_| Ok((exchange1_pubkey, exchange2.clone())));
        let exchange3 = exchange1.clone();
        client
            .expect_get_metro()
            .with(predicate::eq(GetMetroCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((exchange1_pubkey, exchange3.clone())));
        client
            .expect_get_metro()
            .returning(move |_| Err(eyre::eyre!("not found")));

        client.expect_list_metro().returning(move |_| {
            let mut list = HashMap::new();
            list.insert(exchange1_pubkey, exchange1.clone());
            Ok(list)
        });

        // Expected failure
        let mut output = Vec::new();
        let res = GetMetroCliCommand {
            code: Pubkey::new_unique().to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());

        // Expected success by pubkey (table)
        let mut output = Vec::new();
        let res = GetMetroCliCommand {
            code: exchange1_pubkey.to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("account", &exchange1_pubkey.to_string()),
            "account row should contain pubkey"
        );
        assert!(
            has_row("device1", "TestDevice"),
            "device1 row should contain device code"
        );
        assert!(
            has_row("status", "activated"),
            "status row should contain value"
        );

        // Expected success by code (JSON)
        let mut output = Vec::new();
        let res = GetMetroCliCommand {
            code: "test".to_string(),
            json: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(
            json["account"].as_str().unwrap(),
            exchange1_pubkey.to_string()
        );
        assert_eq!(json["code"].as_str().unwrap(), "test");
        assert_eq!(json["name"].as_str().unwrap(), "Test Metro");
        assert_eq!(json["status"].as_str().unwrap(), "activated");
        assert_eq!(json["bgp_community"].as_u64().unwrap(), 1);
        assert_eq!(json["device1"].as_str().unwrap(), "TestDevice");
        assert_eq!(json["device2"].as_str().unwrap(), "(none)");
    }
}
