use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::{serializer, types::NetworkV4List};
use doublezero_sdk::{
    commands::{
        contributor::list::ListContributorCommand, device::list::ListDeviceCommand,
        exchange::list::ListExchangeCommand, location::list::ListLocationCommand,
    },
    DeviceStatus, DeviceType,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListDeviceCliCommand {
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
    pub mgmt_vrf: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListDeviceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let contributors = client.list_contributor(ListContributorCommand {})?;
        let locations = client.list_location(ListLocationCommand {})?;
        let exchanges = client.list_exchange(ListExchangeCommand {})?;
        let devices = client.list_device(ListDeviceCommand)?;

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
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
        };

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            Ok(devices)
        });

        let mut output = Vec::new();
        let res = ListDeviceCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code         | contributor       | location       | exchange       | device_type | public_ip | dz_prefixes | users | max_users | status    | mgmt_vrf | owner                                     \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | device1_code | contributor1_code | location1_code | exchange1_code | switch      | 1.2.3.4   | 1.2.3.4/32  | 0     | 255       | activated | default  | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB \n");

        let mut output = Vec::new();
        let res = ListDeviceCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\",\"code\":\"device1_code\",\"bump_seed\":2,\"location_pk\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR\",\"contributor_code\":\"contributor1_code\",\"location_code\":\"location1_code\",\"location_name\":\"location1_name\",\"exchange_pk\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA\",\"exchange_code\":\"exchange1_code\",\"exchange_name\":\"exchange1_name\",\"device_type\":\"Switch\",\"public_ip\":\"1.2.3.4\",\"dz_prefixes\":\"1.2.3.4/32\",\"users\":0,\"max_users\":255,\"status\":\"Activated\",\"mgmt_vrf\":\"default\",\"owner\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\"}]\n");
    }
}
