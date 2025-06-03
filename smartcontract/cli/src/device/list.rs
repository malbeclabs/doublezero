use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use doublezero_sdk::commands::exchange::list::ListExchangeCommand;
use doublezero_sdk::commands::location::list::ListLocationCommand;
use doublezero_sdk::*;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListDeviceCliCommand {
    #[arg(long, default_value_t = false)]
    pub json: bool,
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct DeviceDisplay {
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    #[tabled(skip)]
    pub bump_seed: u8,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub location_pk: Pubkey,
    #[tabled(rename = "location")]
    pub location_code: String,
    #[tabled(skip)]
    pub location_name: String,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub exchange_pk: Pubkey,
    #[tabled(rename = "exchange")]
    pub exchange_code: String,
    #[tabled(skip)]
    pub exchange_name: String,
    pub device_type: DeviceType,
    #[tabled(display = "doublezero_sla_program::types::ipv4_to_string")]
    #[serde(serialize_with = "crate::serializer::serialize_ipv4_as_string")]
    pub public_ip: IpV4,
    #[tabled(display = "doublezero_sla_program::types::networkv4_list_to_string")]
    #[serde(serialize_with = "crate::serializer::serialize_networkv4list_as_string")]
    pub dz_prefixes: NetworkV4List,
    pub status: DeviceStatus,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListDeviceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let locations = client.list_location(ListLocationCommand {})?;
        let exchanges = client.list_exchange(ListExchangeCommand {})?;

        let devices = client.list_device(ListDeviceCommand {})?;

        let mut devices: Vec<(Pubkey, Device)> = devices.into_iter().collect();
        devices.sort_by(|(_, a), (_, b)| a.owner.cmp(&b.owner));

        let device_displays: Vec<DeviceDisplay> = devices
            .iter()
            .map(|(pubkey, device)| {
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
                    account: *pubkey,
                    code: device.code.clone(),
                    bump_seed: device.bump_seed,
                    location_pk: device.location_pk,
                    location_code,
                    location_name,
                    exchange_pk: device.exchange_pk,
                    exchange_code,
                    exchange_name,
                    device_type: device.device_type,
                    public_ip: device.public_ip,
                    status: device.status,
                    dz_prefixes: device.dz_prefixes.clone(),
                    owner: device.owner,
                }
            })
            .collect();

        let res = if self.json {
            serde_json::to_string_pretty(&device_displays)?
        } else if self.json_compact {
            serde_json::to_string(&device_displays)?
        } else {
            Table::new(device_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{}", res)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::device::list::ListDeviceCliCommand;
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::{
        AccountType, Device, DeviceStatus, DeviceType, Exchange, ExchangeStatus, Location,
        LocationStatus,
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
            code: "location1_code".to_string(),
            name: "location1_name".to_string(),
            country: "location1_country".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
            status: LocationStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR"),
        };

        let exchange1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA");
        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 2,
            code: "exchange1_code".to_string(),
            name: "exchange1_name".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
            status: ExchangeStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA"),
        };

        let device1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            code: "device1_code".to_string(),
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
        };

        client.expect_list_location().returning(move |_| {
            let mut locations = HashMap::new();
            locations.insert(location1_pubkey, location1.clone());
            Ok(locations)
        });

        client.expect_list_exchange().returning(move |_| {
            let mut exchanges = HashMap::new();
            exchanges.insert(exchange1_pubkey, exchange1.clone());
            Ok(exchanges)
        });

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
        assert_eq!(output_str, " account                                   | code         | location       | exchange       | device_type | public_ip | dz_prefixes | status    | owner                                     \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | device1_code | location1_code | exchange1_code | switch      | 1.2.3.4   | 1.2.3.4/32  | activated | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB \n");

        let mut output = Vec::new();
        let res = ListDeviceCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\",\"code\":\"device1_code\",\"bump_seed\":2,\"location_pk\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR\",\"location_code\":\"location1_code\",\"location_name\":\"location1_name\",\"exchange_pk\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA\",\"exchange_code\":\"exchange1_code\",\"exchange_name\":\"exchange1_name\",\"device_type\":\"Switch\",\"public_ip\":\"1.2.3.4\",\"dz_prefixes\":\"1.2.3.4/32\",\"status\":\"Activated\",\"owner\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\"}]\n");
    }
}
