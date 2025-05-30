use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::location::list::ListLocationCommand;
use doublezero_sdk::*;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{Table, Tabled};

#[derive(Args, Debug)]
pub struct ListLocationCliCommand {
    #[arg(long, default_value_t = false)]
    pub json: bool,
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
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
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let locations = client.list_location(ListLocationCommand {})?;

        let mut locations: Vec<(Pubkey, Location)> = locations.into_iter().collect();

        locations.sort_by(|(_, a), (_, b)| a.owner.cmp(&b.owner));

        let location_displays: Vec<LocationDisplay> = locations
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
            .collect();

        let res = if self.json {
            serde_json::to_string_pretty(&location_displays)?
        } else if self.json_compact {
            serde_json::to_string(&location_displays)?
        } else {
            let table = Table::new(location_displays);
            table.to_string()
        };

        writeln!(out, "{}", res)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::location::list::ListLocationCliCommand;
    use crate::location::list::LocationStatus::Activated;
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::{AccountType, Location};
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_cli_location_list() {
        let mut client = create_test_client();

        let location1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let location1 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 255,
            code: "some code".to_string(),
            name: "some name".to_string(),
            country: "some country".to_string(),
            lat: 15.0,
            lng: 15.0,
            loc_id: 7,
            status: Activated,
            owner: location1_pubkey,
        };
        client.expect_list_location().returning(move |_| {
            let mut locations = HashMap::new();
            locations.insert(location1_pubkey, location1.clone());
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
        assert_eq!(output_str, "+-------------------------------------------+-----------+-----------+--------------+-----+-----+--------+-----------+-------------------------------------------+\n| account                                   | code      | name      | country      | lat | lng | loc_id | status    | owner                                     |\n+-------------------------------------------+-----------+-----------+--------------+-----+-----+--------+-----------+-------------------------------------------+\n| 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | some code | some name | some country | 15  | 15  | 7      | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo |\n+-------------------------------------------+-----------+-----------+--------------+-----+-----+--------+-----------+-------------------------------------------+\n");

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
