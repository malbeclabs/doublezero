use clap::Args;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use doublezero_sdk::commands::exchange::list::ListExchangeCommand;
use doublezero_sdk::commands::location::list::ListLocationCommand;
use doublezero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListDeviceArgs {
    #[arg(long, default_value_t = false)]
    pub json: bool,
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Serialize)]
pub struct DeviceDisplay {
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub bump_seed: u8,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub location_pk: Pubkey,
    pub location_code: String,
    pub location_name: String,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub exchange_pk: Pubkey,
    pub exchange_code: String,
    pub exchange_name: String,
    pub device_type: DeviceType,
    #[serde(serialize_with = "crate::serializer::serialize_ipv4_as_string")]
    pub public_ip: IpV4,
    pub status: DeviceStatus,
    #[serde(serialize_with = "crate::serializer::serialize_networkv4list_as_string")]
    pub dz_prefixes: NetworkV4List,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListDeviceArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let locations = ListLocationCommand {}.execute(client)?;
        let exchanges = ListExchangeCommand {}.execute(client)?;

        let devices = ListDeviceCommand {}.execute(client)?;

        let mut devices: Vec<(Pubkey, Device)> = devices.into_iter().collect();
        devices.sort_by(|(_, a), (_, b)| a.owner.cmp(&b.owner));

        if self.json || self.json_compact {
            let devices = devices
                .into_iter()
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
                        account: pubkey,
                        code: device.code,
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
                        dz_prefixes: device.dz_prefixes,
                        owner: device.owner,
                    }
                })
                .collect::<Vec<_>>();

            let json = {
                if self.json_compact {
                    serde_json::to_string(&devices)?
                } else {
                    serde_json::to_string_pretty(&devices)?
                }
            };
            writeln!(out, "{}", json)?;
        } else {
            let mut table = Table::new();
            table.add_row(row![
                "account",
                "code",
                "location",
                "exchange",
                "device_type",
                "public_ip",
                "dz_prefixes",
                "status",
                "owner"
            ]);
            for (pubkey, data) in devices {
                let loc_name = match &locations.get(&data.location_pk) {
                    Some(location) => &location.code,
                    None => &data.location_pk.to_string(),
                };
                let exch_name = match &exchanges.get(&data.exchange_pk) {
                    Some(exchange) => &exchange.code,
                    None => &data.exchange_pk.to_string(),
                };

                table.add_row(Row::new(vec![
                    Cell::new(&pubkey.to_string()),
                    Cell::new(&data.code),
                    Cell::new(loc_name),
                    Cell::new(exch_name),
                    Cell::new(&data.device_type.to_string()),
                    Cell::new(&ipv4_to_string(&data.public_ip)),
                    Cell::new(&networkv4_list_to_string(&data.dz_prefixes)),
                    Cell::new(&data.status.to_string()),
                    Cell::new(&data.owner.to_string()),
                ]));
            }

            table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
            table.print(out)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::device::list::ListDeviceArgs;
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::{
        AccountType, Device, DeviceStatus, DeviceType, Exchange, ExchangeStatus, Location,
        LocationStatus,
    };

    use doublezero_sla_program::state::accountdata::AccountData;
    use mockall::predicate;
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

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Location))
            .returning(move |_| {
                let mut locations = HashMap::new();
                locations.insert(location1_pubkey, AccountData::Location(location1.clone()));
                Ok(locations)
            });

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Exchange))
            .returning(move |_| {
                let mut exchanges = HashMap::new();
                exchanges.insert(exchange1_pubkey, AccountData::Exchange(exchange1.clone()));
                Ok(exchanges)
            });

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Device))
            .returning(move |_| {
                let mut devices = HashMap::new();
                devices.insert(device1_pubkey, AccountData::Device(device1.clone()));
                Ok(devices)
            });

        let mut output = Vec::new();
        let res = ListDeviceArgs {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code         | location       | exchange       | device_type | public_ip | dz_prefixes | status    | owner \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | device1_code | location1_code | exchange1_code | switch      | 1.2.3.4   | 1.2.3.4/32  | activated | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB \n");

        let mut output = Vec::new();
        let res = ListDeviceArgs {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\",\"code\":\"device1_code\",\"bump_seed\":2,\"location_pk\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR\",\"location_code\":\"location1_code\",\"location_name\":\"location1_name\",\"exchange_pk\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPA\",\"exchange_code\":\"exchange1_code\",\"exchange_name\":\"exchange1_name\",\"device_type\":\"Switch\",\"public_ip\":\"1.2.3.4\",\"status\":\"Activated\",\"dz_prefixes\":\"1.2.3.4/32\",\"owner\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\"}]\n");
    }
}
