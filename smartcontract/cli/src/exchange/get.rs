use crate::{doublezerocommand::CliCommand, validators::validate_code};
use clap::Args;
use doublezero_sdk::commands::{
    device::list::ListDeviceCommand, exchange::get::GetExchangeCommand,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetExchangeCliCommand {
    /// Exchange Pubkey or code to get details for
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
    pub device1: String,
    pub device2: String,
    pub lat: f64,
    pub lng: f64,
    pub bgp_community: u16,
    pub reference_count: u32,
    pub status: String,
    pub owner: String,
}

impl GetExchangeCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, exchange) = client.get_exchange(GetExchangeCommand {
            pubkey_or_code: self.code,
        })?;

        let devices = client.list_device(ListDeviceCommand)?;

        let device1 = if exchange.device1_pk == Pubkey::default() {
            "(none)".to_string()
        } else {
            devices
                .get(&exchange.device1_pk)
                .map_or_else(|| exchange.device1_pk.to_string(), |d| d.code.clone())
        };
        let device2 = if exchange.device2_pk == Pubkey::default() {
            "(none)".to_string()
        } else {
            devices
                .get(&exchange.device2_pk)
                .map_or_else(|| exchange.device2_pk.to_string(), |d| d.code.clone())
        };

        let display = ExchangeDisplay {
            account: pubkey.to_string(),
            code: exchange.code,
            name: exchange.name,
            device1,
            device2,
            lat: exchange.lat,
            lng: exchange.lng,
            bgp_community: exchange.bgp_community,
            reference_count: exchange.reference_count,
            status: exchange.status.to_string(),
            owner: exchange.owner.to_string(),
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
    use crate::{exchange::get::GetExchangeCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{
        commands::{device::list::ListDeviceCommand, exchange::get::GetExchangeCommand},
        AccountType, Device, DeviceStatus, DeviceType, Exchange, ExchangeStatus,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, str::FromStr};

    #[test]
    fn test_cli_exchange_get() {
        let mut client = create_test_client();

        let contributor_pubkey =
            Pubkey::from_str("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx").unwrap();
        let location_pubkey =
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
            location_pk: location_pubkey,
            exchange_pk: exchange1_pubkey,
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
            multicast_users_count: 0,
            max_unicast_users: 0,
            max_multicast_users: 0,
            reserved_seats: 0,
        };

        client
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand {}))
            .returning(move |_| {
                let mut devices = HashMap::new();
                devices.insert(device1_pubkey, device1.clone());
                Ok(devices)
            });

        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            device1_pk: device1_pubkey,
            device2_pk: Pubkey::default(),
            lat: 12.34,
            lng: 56.78,
            bgp_community: 1,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: exchange1_pubkey,
        };

        let exchange2 = exchange1.clone();
        client
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: exchange1_pubkey.to_string(),
            }))
            .returning(move |_| Ok((exchange1_pubkey, exchange2.clone())));
        let exchange3 = exchange1.clone();
        client
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((exchange1_pubkey, exchange3.clone())));
        client
            .expect_get_exchange()
            .returning(move |_| Err(eyre::eyre!("not found")));

        client.expect_list_exchange().returning(move |_| {
            let mut list = HashMap::new();
            list.insert(exchange1_pubkey, exchange1.clone());
            Ok(list)
        });

        // Expected failure
        let mut output = Vec::new();
        let res = GetExchangeCliCommand {
            code: Pubkey::new_unique().to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());

        // Expected success by pubkey (table)
        let mut output = Vec::new();
        let res = GetExchangeCliCommand {
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
        let res = GetExchangeCliCommand {
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
        assert_eq!(json["name"].as_str().unwrap(), "Test Exchange");
        assert_eq!(json["status"].as_str().unwrap(), "activated");
        assert_eq!(json["bgp_community"].as_u64().unwrap(), 1);
        assert_eq!(json["device1"].as_str().unwrap(), "TestDevice");
        assert_eq!(json["device2"].as_str().unwrap(), "(none)");
    }
}
