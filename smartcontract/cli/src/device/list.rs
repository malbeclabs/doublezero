use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::{serializer, types::NetworkV4List};
use doublezero_sdk::{
    commands::{
        contributor::{get::GetContributorCommand, list::ListContributorCommand},
        device::list::ListDeviceCommand,
        exchange::list::ListExchangeCommand,
        location::list::ListLocationCommand,
    },
    DeviceStatus, DeviceType,
};
use doublezero_serviceability::state::device::{DeviceDesiredStatus, DeviceHealth};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr, str::FromStr};
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListDeviceCliCommand {
    /// Filter by contributor (pubkey or code)
    #[arg(long, short = 'c')]
    pub contributor: Option<String>,
    /// Filter by exchange (pubkey or code)
    #[arg(long)]
    pub exchange: Option<String>,
    /// Filter by location (pubkey or code)
    #[arg(long)]
    pub location: Option<String>,
    /// Filter by device type (hybrid, transit, edge)
    #[arg(long)]
    pub device_type: Option<String>,
    /// Filter by status (pending, activated, deleting, rejected, drained, device-provisioning, link-provisioning)
    #[arg(long)]
    pub status: Option<String>,
    /// Filter by health (unknown, pending, ready-for-links, ready-for-users, impaired)
    #[arg(long)]
    pub health: Option<String>,
    /// Filter by desired status (pending, activated, drained)
    #[arg(long)]
    pub desired_status: Option<String>,
    /// Filter by device code (partial match)
    #[arg(long)]
    pub code: Option<String>,
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct DeviceDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    #[tabled(skip)]
    pub bump_seed: u8,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub location_pk: Pubkey,
    #[tabled(rename = "contributor")]
    pub contributor_code: String,
    #[tabled(rename = "location")]
    pub location_code: String,
    #[tabled(skip)]
    pub location_name: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub exchange_pk: Pubkey,
    #[tabled(rename = "exchange")]
    pub exchange_code: String,
    #[tabled(skip)]
    pub exchange_name: String,
    pub device_type: DeviceType,
    pub public_ip: Ipv4Addr,
    #[tabled(display = "doublezero_program_common::types::NetworkV4List::to_string")]
    #[serde(serialize_with = "serializer::serialize_networkv4list_as_string")]
    pub dz_prefixes: NetworkV4List,
    pub users: u16,
    pub max_users: u16,
    pub status: DeviceStatus,
    pub health: DeviceHealth,
    #[tabled(skip)]
    pub desired_status: DeviceDesiredStatus,
    pub mgmt_vrf: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub metrics_publisher_pk: Pubkey,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub owner: Pubkey,
}

impl ListDeviceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let contributors = client.list_contributor(ListContributorCommand {})?;
        let locations = client.list_location(ListLocationCommand {})?;
        let exchanges = client.list_exchange(ListExchangeCommand {})?;
        let mut devices = client.list_device(ListDeviceCommand)?;

        // Filter by contributor if specified
        if let Some(contributor_filter) = &self.contributor {
            let contributor_pk = match client.get_contributor(GetContributorCommand {
                pubkey_or_code: contributor_filter.clone(),
            }) {
                Ok((pk, _)) => pk,
                Err(_) => {
                    return Err(eyre::eyre!(
                        "Contributor '{}' not found",
                        contributor_filter
                    ));
                }
            };
            devices.retain(|_, device| device.contributor_pk == contributor_pk);
        }

        // Filter by exchange if specified
        if let Some(exchange_filter) = &self.exchange {
            let exchange_pk = exchanges
                .iter()
                .find(|(pk, ex)| pk.to_string() == *exchange_filter || ex.code == *exchange_filter)
                .map(|(pk, _)| *pk)
                .ok_or_else(|| eyre::eyre!("Exchange '{}' not found", exchange_filter))?;
            devices.retain(|_, device| device.exchange_pk == exchange_pk);
        }

        // Filter by location if specified
        if let Some(location_filter) = &self.location {
            let location_pk = locations
                .iter()
                .find(|(pk, loc)| {
                    pk.to_string() == *location_filter || loc.code == *location_filter
                })
                .map(|(pk, _)| *pk)
                .ok_or_else(|| eyre::eyre!("Location '{}' not found", location_filter))?;
            devices.retain(|_, device| device.location_pk == location_pk);
        }

        // Filter by device type if specified
        if let Some(device_type_filter) = &self.device_type {
            let device_type = DeviceType::from_str(device_type_filter)
                .map_err(|e| eyre::eyre!("Invalid device type '{}': {}", device_type_filter, e))?;
            devices.retain(|_, device| device.device_type == device_type);
        }

        // Filter by status if specified
        if let Some(status_filter) = &self.status {
            let status = DeviceStatus::from_str(status_filter)
                .map_err(|e| eyre::eyre!("Invalid status '{}': {}", status_filter, e))?;
            devices.retain(|_, device| device.status == status);
        }

        // Filter by health if specified
        if let Some(health_filter) = &self.health {
            let health = DeviceHealth::from_str(health_filter)
                .map_err(|e| eyre::eyre!("Invalid health '{}': {}", health_filter, e))?;
            devices.retain(|_, device| device.device_health == health);
        }

        // Filter by desired status if specified
        if let Some(desired_status_filter) = &self.desired_status {
            let desired_status =
                DeviceDesiredStatus::from_str(desired_status_filter).map_err(|e| {
                    eyre::eyre!("Invalid desired status '{}': {}", desired_status_filter, e)
                })?;
            devices.retain(|_, device| device.desired_status == desired_status);
        }

        // Filter by code if specified (partial match)
        if let Some(code_filter) = &self.code {
            devices.retain(|_, device| device.code.contains(code_filter));
        }

        let mut device_displays: Vec<DeviceDisplay> = devices
            .into_iter()
            .map(|(pubkey, device)| {
                let contributor_code = match contributors.get(&device.contributor_pk) {
                    Some(contributor) => contributor.code.clone(),
                    None => device.contributor_pk.to_string(),
                };
                let (location_code, location_name) = match locations.get(&device.location_pk) {
                    Some(location) => (location.code.clone(), location.name.clone()),
                    None => (
                        device.location_pk.to_string(),
                        device.location_pk.to_string(),
                    ),
                };
                let (exchange_code, exchange_name) = match exchanges.get(&device.exchange_pk) {
                    Some(exchange) => (exchange.code.clone(), exchange.name.clone()),
                    None => (
                        device.exchange_pk.to_string(),
                        device.exchange_pk.to_string(),
                    ),
                };

                DeviceDisplay {
                    account: pubkey,
                    code: device.code.clone(),
                    bump_seed: device.bump_seed,
                    location_pk: device.location_pk,
                    contributor_code,
                    location_code,
                    location_name,
                    exchange_pk: device.exchange_pk,
                    exchange_code,
                    exchange_name,
                    device_type: device.device_type,
                    public_ip: device.public_ip,
                    status: device.status,
                    dz_prefixes: device.dz_prefixes.clone(),
                    mgmt_vrf: device.mgmt_vrf.clone(),
                    users: device.users_count,
                    max_users: device.max_users,
                    health: device.device_health,
                    desired_status: device.desired_status,
                    metrics_publisher_pk: device.metrics_publisher_pk,
                    owner: device.owner,
                }
            })
            .collect();

        device_displays.sort_by(|a, b| {
            a.exchange_name
                .cmp(&b.exchange_name)
                .then_with(|| a.code.cmp(&b.code))
        });

        let res = if self.json {
            serde_json::to_string_pretty(&device_displays)?
        } else if self.json_compact {
            serde_json::to_string(&device_displays)?
        } else {
            Table::new(device_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{device::list::ListDeviceCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{
        AccountType, Contributor, ContributorStatus, Device, DeviceStatus, DeviceType, Exchange,
        ExchangeStatus, Location, LocationStatus,
    };
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_device_list() {
        let mut client = create_test_client();

        let location1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR");
        let location1 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "location1_code".to_string(),
            name: "location1_name".to_string(),
            country: "location1_country".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
            status: LocationStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR"),
        };

        client.expect_list_location().returning(move |_| {
            let mut locations = HashMap::new();
            locations.insert(location1_pubkey, location1.clone());
            Ok(locations)
        });

        let exchange1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA");
        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "exchange1_code".to_string(),
            name: "exchange1_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 1.0,
            lng: 2.0,
            bgp_community: 3,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA"),
        };

        client.expect_list_exchange().returning(move |_| {
            let mut exchanges = HashMap::new();
            exchanges.insert(exchange1_pubkey, exchange1.clone());
            Ok(exchanges)
        });

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let contributor = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "contributor1_code".to_string(),
            status: ContributorStatus::Activated,
            owner: contributor_pk,
            ops_manager_pk: Pubkey::default(),
        };

        client.expect_list_contributor().returning(move |_| {
            let mut contributors = HashMap::new();
            contributors.insert(contributor_pk, contributor.clone());
            Ok(contributors)
        });

        let device1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "device1_code".to_string(),
            contributor_pk,
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            Ok(devices)
        });

        let mut output = Vec::new();
        let res = ListDeviceCliCommand {
            contributor: None,
            exchange: None,
            location: None,
            device_type: None,
            status: None,
            health: None,
            desired_status: None,
            code: None,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code         | contributor       | location       | exchange       | device_type | public_ip | dz_prefixes | users | max_users | status    | health          | mgmt_vrf \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | device1_code | contributor1_code | location1_code | exchange1_code | hybrid      | 1.2.3.4   | 1.2.3.4/32  | 0     | 255       | activated | ready-for-users | default  \n");

        let mut output = Vec::new();
        let res = ListDeviceCliCommand {
            contributor: None,
            exchange: None,
            location: None,
            device_type: None,
            status: None,
            health: None,
            desired_status: None,
            code: None,
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\",\"code\":\"device1_code\",\"bump_seed\":2,\"location_pk\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR\",\"contributor_code\":\"contributor1_code\",\"location_code\":\"location1_code\",\"location_name\":\"location1_name\",\"exchange_pk\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA\",\"exchange_code\":\"exchange1_code\",\"exchange_name\":\"exchange1_name\",\"device_type\":\"Hybrid\",\"public_ip\":\"1.2.3.4\",\"dz_prefixes\":\"1.2.3.4/32\",\"users\":0,\"max_users\":255,\"status\":\"Activated\",\"health\":\"ReadyForUsers\",\"desired_status\":\"Activated\",\"mgmt_vrf\":\"default\",\"metrics_publisher_pk\":\"11111111111111111111111111111111\",\"owner\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\"}]\n");
    }

    #[test]
    fn test_cli_device_list_filter_by_device_type() {
        let mut client = create_test_client();

        let location1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR");
        let location1 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "location1_code".to_string(),
            name: "location1_name".to_string(),
            country: "location1_country".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
            status: LocationStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR"),
        };

        client.expect_list_location().returning(move |_| {
            let mut locations = HashMap::new();
            locations.insert(location1_pubkey, location1.clone());
            Ok(locations)
        });

        let exchange1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA");
        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "exchange1_code".to_string(),
            name: "exchange1_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 1.0,
            lng: 2.0,
            bgp_community: 3,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA"),
        };

        client.expect_list_exchange().returning(move |_| {
            let mut exchanges = HashMap::new();
            exchanges.insert(exchange1_pubkey, exchange1.clone());
            Ok(exchanges)
        });

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let contributor = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "contributor1_code".to_string(),
            status: ContributorStatus::Activated,
            owner: contributor_pk,
            ops_manager_pk: Pubkey::default(),
        };

        client.expect_list_contributor().returning(move |_| {
            let mut contributors = HashMap::new();
            contributors.insert(contributor_pk, contributor.clone());
            Ok(contributors)
        });

        let device1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "device1_hybrid".to_string(),
            contributor_pk,
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };

        let device2_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPD");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 2,
            bump_seed: 3,
            reference_count: 0,
            code: "device2_transit".to_string(),
            contributor_pk,
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Transit,
            public_ip: [5, 6, 7, 8].into(),
            dz_prefixes: "5.6.7.8/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPD"),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            devices.insert(device2_pubkey, device2.clone());
            Ok(devices)
        });

        // Test filter by device_type=hybrid (should return only device1)
        let mut output = Vec::new();
        let res = ListDeviceCliCommand {
            contributor: None,
            exchange: None,
            location: None,
            device_type: Some("hybrid".to_string()),
            status: None,
            health: None,
            desired_status: None,
            code: None,
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("device1_hybrid"));
        assert!(!output_str.contains("device2_transit"));
    }

    #[test]
    fn test_cli_device_list_filter_by_code() {
        let mut client = create_test_client();

        let location1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR");
        let location1 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "location1_code".to_string(),
            name: "location1_name".to_string(),
            country: "location1_country".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
            status: LocationStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR"),
        };

        client.expect_list_location().returning(move |_| {
            let mut locations = HashMap::new();
            locations.insert(location1_pubkey, location1.clone());
            Ok(locations)
        });

        let exchange1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA");
        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "exchange1_code".to_string(),
            name: "exchange1_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 1.0,
            lng: 2.0,
            bgp_community: 3,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA"),
        };

        client.expect_list_exchange().returning(move |_| {
            let mut exchanges = HashMap::new();
            exchanges.insert(exchange1_pubkey, exchange1.clone());
            Ok(exchanges)
        });

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let contributor = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "contributor1_code".to_string(),
            status: ContributorStatus::Activated,
            owner: contributor_pk,
            ops_manager_pk: Pubkey::default(),
        };

        client.expect_list_contributor().returning(move |_| {
            let mut contributors = HashMap::new();
            contributors.insert(contributor_pk, contributor.clone());
            Ok(contributors)
        });

        let device1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "ams-device-001".to_string(),
            contributor_pk,
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };

        let device2_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPD");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 2,
            bump_seed: 3,
            reference_count: 0,
            code: "nyc-device-002".to_string(),
            contributor_pk,
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Transit,
            public_ip: [5, 6, 7, 8].into(),
            dz_prefixes: "5.6.7.8/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPD"),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            devices.insert(device2_pubkey, device2.clone());
            Ok(devices)
        });

        // Test filter by code=ams (should return only device1)
        let mut output = Vec::new();
        let res = ListDeviceCliCommand {
            contributor: None,
            exchange: None,
            location: None,
            device_type: None,
            status: None,
            health: None,
            desired_status: None,
            code: Some("ams".to_string()),
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("ams-device-001"));
        assert!(!output_str.contains("nyc-device-002"));
    }

    #[test]
    fn test_cli_device_list_filter_by_status() {
        let mut client = create_test_client();

        let location1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR");
        let location1 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "location1_code".to_string(),
            name: "location1_name".to_string(),
            country: "location1_country".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
            status: LocationStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR"),
        };

        client.expect_list_location().returning(move |_| {
            let mut locations = HashMap::new();
            locations.insert(location1_pubkey, location1.clone());
            Ok(locations)
        });

        let exchange1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA");
        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "exchange1_code".to_string(),
            name: "exchange1_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 1.0,
            lng: 2.0,
            bgp_community: 3,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA"),
        };

        client.expect_list_exchange().returning(move |_| {
            let mut exchanges = HashMap::new();
            exchanges.insert(exchange1_pubkey, exchange1.clone());
            Ok(exchanges)
        });

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let contributor = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "contributor1_code".to_string(),
            status: ContributorStatus::Activated,
            owner: contributor_pk,
            ops_manager_pk: Pubkey::default(),
        };

        client.expect_list_contributor().returning(move |_| {
            let mut contributors = HashMap::new();
            contributors.insert(contributor_pk, contributor.clone());
            Ok(contributors)
        });

        let device1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "device1_activated".to_string(),
            contributor_pk,
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };

        let device2_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPD");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 2,
            bump_seed: 3,
            reference_count: 0,
            code: "device2_pending".to_string(),
            contributor_pk,
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [5, 6, 7, 8].into(),
            dz_prefixes: "5.6.7.8/32".parse().unwrap(),
            status: DeviceStatus::Pending,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPD"),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::Pending,
            desired_status: doublezero_serviceability::state::device::DeviceDesiredStatus::Pending,
        };

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            devices.insert(device2_pubkey, device2.clone());
            Ok(devices)
        });

        // Test filter by status=activated (should return only device1)
        let mut output = Vec::new();
        let res = ListDeviceCliCommand {
            contributor: None,
            exchange: None,
            location: None,
            device_type: None,
            status: Some("activated".to_string()),
            health: None,
            desired_status: None,
            code: None,
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("device1_activated"));
        assert!(!output_str.contains("device2_pending"));
    }

    #[test]
    fn test_cli_device_list_filter_combined() {
        let mut client = create_test_client();

        let location1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR");
        let location1 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "ams".to_string(),
            name: "Amsterdam".to_string(),
            country: "NL".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
            status: LocationStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR"),
        };

        let location2_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPS");
        let location2 = Location {
            account_type: AccountType::Location,
            index: 2,
            bump_seed: 3,
            reference_count: 0,
            code: "nyc".to_string(),
            name: "New York".to_string(),
            country: "US".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 4,
            status: LocationStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPS"),
        };

        client.expect_list_location().returning(move |_| {
            let mut locations = HashMap::new();
            locations.insert(location1_pubkey, location1.clone());
            locations.insert(location2_pubkey, location2.clone());
            Ok(locations)
        });

        let exchange1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA");
        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "exchange1_code".to_string(),
            name: "exchange1_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 1.0,
            lng: 2.0,
            bgp_community: 3,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA"),
        };

        client.expect_list_exchange().returning(move |_| {
            let mut exchanges = HashMap::new();
            exchanges.insert(exchange1_pubkey, exchange1.clone());
            Ok(exchanges)
        });

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let contributor = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "contributor1_code".to_string(),
            status: ContributorStatus::Activated,
            owner: contributor_pk,
            ops_manager_pk: Pubkey::default(),
        };

        client.expect_list_contributor().returning(move |_| {
            let mut contributors = HashMap::new();
            contributors.insert(contributor_pk, contributor.clone());
            Ok(contributors)
        });

        let device1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "ams-device-001".to_string(),
            contributor_pk,
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };

        let device2_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPD");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 2,
            bump_seed: 3,
            reference_count: 0,
            code: "nyc-device-002".to_string(),
            contributor_pk,
            location_pk: location2_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Transit,
            public_ip: [5, 6, 7, 8].into(),
            dz_prefixes: "5.6.7.8/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPD"),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            devices.insert(device2_pubkey, device2.clone());
            Ok(devices)
        });

        // Test combined filters: location=ams AND device_type=hybrid AND status=activated
        let mut output = Vec::new();
        let res = ListDeviceCliCommand {
            contributor: None,
            exchange: None,
            location: Some("ams".to_string()),
            device_type: Some("hybrid".to_string()),
            status: Some("activated".to_string()),
            health: None,
            desired_status: None,
            code: None,
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("ams-device-001"));
        assert!(!output_str.contains("nyc-device-002"));
    }
}
