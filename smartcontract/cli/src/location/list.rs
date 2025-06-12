use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::{commands::location::list::ListLocationCommand, *};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListLocationCliCommand {
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct LocationDisplay {
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub name: String,
    pub country: String,
    pub lat: f64,
    pub lng: f64,
    pub status: LocationStatus,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
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
                status: tunnel.status,
                owner: tunnel.owner,
            })
            .collect();

        let res = if self.json {
            serde_json::to_string_pretty(&location_displays)?
        } else if self.json_compact {
            serde_json::to_string(&location_displays)?
        } else {
            Table::new(location_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        location::list::{ListLocationCliCommand, LocationStatus::Activated},
        tests::utils::create_test_client,
    };
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
        assert_eq!(output_str, " account                                   | code      | name      | country      | lat | lng | status    | owner                                     \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | some code | some name | some country | 15  | 15  | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n");

        let mut output = Vec::new();
        let res = ListLocationCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\",\"code\":\"some code\",\"name\":\"some name\",\"country\":\"some country\",\"lat\":15.0,\"lng\":15.0,\"status\":\"Activated\",\"owner\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\"}]\n");
    }
}
