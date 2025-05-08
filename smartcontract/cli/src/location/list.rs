use clap::Args;
use doublezero_sdk::commands::location::list::ListLocationCommand;
use doublezero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListLocationCliCommand {
    #[arg(long, default_value_t = false)]
    pub json: bool,
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Serialize)]
pub struct LocationDisplay {
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub account: Pubkey, // 32
    pub code: String,           // 4 + len
    pub name: String,           // 4 + len
    pub country: String,        // 4 + len
    pub lat: f64,               // 8
    pub lng: f64,               // 8
    pub loc_id: u32,            // 4
    pub status: LocationStatus, // 1
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey, // 32
}

impl ListLocationCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let locations = ListLocationCommand {}.execute(client)?;

        let mut locations: Vec<(Pubkey, Location)> = locations.into_iter().collect();

        locations.sort_by(|(_, a), (_, b)| a.owner.cmp(&b.owner));

        if self.json || self.json_compact {
            let locations = locations
                .into_iter()
                .map(|(pubkey, tunnel)| LocationDisplay {
                    account: pubkey,
                    code: tunnel.code,
                    name: tunnel.name,
                    country: tunnel.country,
                    lat: tunnel.lat,
                    lng: tunnel.lng,
                    loc_id: tunnel.loc_id,
                    status: tunnel.status,
                    owner: tunnel.owner,
                })
                .collect::<Vec<_>>();

            let json = {
                if self.json_compact {
                    serde_json::to_string(&locations)?
                } else {
                    serde_json::to_string_pretty(&locations)?
                }
            };
            writeln!(out, "{}", json)?;
        } else {
            let mut table = Table::new();
            table.add_row(row![
                "account", "code", "name", "country", "lat", "lng", "loc_id", "status", "owner"
            ]);

            for (pubkey, data) in locations {
                table.add_row(Row::new(vec![
                    Cell::new(&pubkey.to_string()),
                    Cell::new(&data.code),
                    Cell::new(&data.name),
                    Cell::new(&data.country),
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

    use crate::location::list::ListLocationCliCommand;
    use crate::location::list::LocationStatus::Activated;
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::{AccountType, Device, DeviceStatus, DeviceType, Location};

    use doublezero_sla_program::state::accountdata::AccountData;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_location_list() {
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

        let location1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let location1 = Location {
            account_type: AccountType::Location,
            owner: location1_pubkey,
            index: 1,
            bump_seed: 2,
            lat: 15.00,
            lng: 15.00,
            loc_id: 7,
            code: "some code".to_string(),
            name: "some name".to_string(),
            country: "some country".to_string(),
            status: Activated,
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Location))
            .returning(move |_| {
                let mut locations = HashMap::new();
                locations.insert(location1_pubkey, AccountData::Location(location1.clone()));
                Ok(locations)
            });

        let mut output = Vec::new();
        let res = ListLocationCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code      | name      | country      | lat | lng | loc_id | status    | owner \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | some code | some name | some country | 15  | 15  | 7      | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n");

        let mut output = Vec::new();
        let res = ListLocationCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\",\"code\":\"some code\",\"name\":\"some name\",\"country\":\"some country\",\"lat\":15.0,\"lng\":15.0,\"loc_id\":7,\"status\":\"Activated\",\"owner\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\"}]\n");
    }
}
