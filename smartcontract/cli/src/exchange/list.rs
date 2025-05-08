use clap::Args;
use doublezero_sdk::commands::exchange::list::ListExchangeCommand;
use doublezero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListExchangeCliCommand {
    #[arg(long, default_value_t = false)]
    pub json: bool,
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Serialize)]
pub struct ExchangeDisplay {
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub name: String,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: u32,
    pub status: ExchangeStatus,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListExchangeCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let exchanges = ListExchangeCommand {}.execute(client)?;

        let mut exchanges: Vec<(Pubkey, Exchange)> = exchanges.into_iter().collect();
        exchanges.sort_by(|(_, a), (_, b)| a.owner.cmp(&b.owner));

        if self.json || self.json_compact {
            let exchanges = exchanges
                .into_iter()
                .map(|(pubkey, tunnel)| ExchangeDisplay {
                    account: pubkey,
                    code: tunnel.code,
                    name: tunnel.name,
                    lat: tunnel.lat,
                    lng: tunnel.lng,
                    loc_id: tunnel.loc_id,
                    status: tunnel.status,
                    owner: tunnel.owner,
                })
                .collect::<Vec<_>>();

            let json = {
                if self.json_compact {
                    serde_json::to_string(&exchanges)?
                } else {
                    serde_json::to_string_pretty(&exchanges)?
                }
            };
            writeln!(out, "{}", json)?;
        } else {
            let mut table = Table::new();
            table.add_row(row![
                "account", "code", "name", "lat", "lng", "loc_id", "status", "owner"
            ]);

            for (pubkey, data) in exchanges {
                table.add_row(Row::new(vec![
                    Cell::new(&pubkey.to_string()),
                    Cell::new(&data.code),
                    Cell::new(&data.name),
                    Cell::new(&data.lat.to_string()),
                    Cell::new(&data.lng.to_string()),
                    Cell::new(&data.loc_id.to_string()),
                    Cell::new(&data.status.to_string()),
                    Cell::new(&data.owner.to_string()),
                ]));
            }

            table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
            let _ = table.print(out);
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::exchange::list::ExchangeStatus::Activated;
    use crate::{exchange::list::ListExchangeCliCommand, tests::tests::create_test_client};
    use doublezero_sdk::{AccountType, Device, DeviceStatus, DeviceType, Exchange};
    use doublezero_sla_program::state::accountdata::AccountData;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_exchange_list() {
        let mut client = create_test_client();

        let location1_pubkey = Pubkey::new_unique();
        let location2_pubkey = Pubkey::new_unique();
        let exchange1_pubkey = Pubkey::new_unique();
        let exchange2_pubkey = Pubkey::new_unique();

        let device1_pubkey = Pubkey::new_unique();
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
            owner: Pubkey::new_unique(),
        };
        let device2_pubkey = Pubkey::new_unique();
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            code: "device2_code".to_string(),
            location_pk: location2_pubkey,
            exchange_pk: exchange2_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Device))
            .returning(move |_| {
                let mut devices = HashMap::new();
                devices.insert(device1_pubkey, AccountData::Device(device1.clone()));
                devices.insert(device2_pubkey, AccountData::Device(device2.clone()));
                Ok(devices)
            });

        let exchange1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            owner: exchange1_pubkey,
            index: 1,
            bump_seed: 2,
            lat: 15.00,
            lng: 15.00,
            loc_id: 6,
            status: Activated,
            code: "some code".to_string(),
            name: "some name".to_string(),
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Exchange))
            .returning(move |_| {
                let mut exchanges = HashMap::new();
                exchanges.insert(exchange1_pubkey, AccountData::Exchange(exchange1.clone()));
                Ok(exchanges)
            });

        let mut output = Vec::new();
        let res = ListExchangeCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code      | name      | lat | lng | loc_id | status    | owner \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | some code | some name | 15  | 15  | 6      | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n");

        let mut output = Vec::new();
        let res = ListExchangeCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\",\"code\":\"some code\",\"name\":\"some name\",\"lat\":15.0,\"lng\":15.0,\"loc_id\":6,\"status\":\"Activated\",\"owner\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\"}]\n");
    }
}
