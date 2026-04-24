use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::{commands::facility::list::ListFacilityCommand, FacilityStatus};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListFacilityCliCommand {
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct FacilityDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub name: String,
    pub country: String,
    pub lat: f64,
    pub lng: f64,
    pub status: FacilityStatus,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListFacilityCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let facilities = client.list_facility(ListFacilityCommand)?;

        let mut facility_displays: Vec<FacilityDisplay> = facilities
            .into_iter()
            .map(|(pubkey, tunnel)| FacilityDisplay {
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

        facility_displays.sort_by(|a, b| a.name.cmp(&b.name).then_with(|| a.code.cmp(&b.code)));

        let res = if self.json {
            serde_json::to_string_pretty(&facility_displays)?
        } else if self.json_compact {
            serde_json::to_string(&facility_displays)?
        } else {
            Table::new(facility_displays)
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
        facility::list::{FacilityStatus::Activated, ListFacilityCliCommand},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{AccountType, Facility};
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_cli_facility_list() {
        let mut client = create_test_client();

        let facility1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let facility1 = Facility {
            account_type: AccountType::Facility,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "some code".to_string(),
            name: "some name".to_string(),
            country: "some country".to_string(),
            lat: 15.0,
            lng: 15.0,
            loc_id: 7,
            status: Activated,
            owner: facility1_pubkey,
        };
        client.expect_list_facility().returning(move |_| {
            let mut facilities = HashMap::new();
            facilities.insert(facility1_pubkey, facility1.clone());
            Ok(facilities)
        });

        let mut output = Vec::new();
        let res = ListFacilityCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code      | name      | country      | lat | lng | status    | owner                                     \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | some code | some name | some country | 15  | 15  | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n");

        let mut output = Vec::new();
        let res = ListFacilityCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\",\"code\":\"some code\",\"name\":\"some name\",\"country\":\"some country\",\"lat\":15.0,\"lng\":15.0,\"status\":\"Activated\",\"owner\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\"}]\n");
    }
}
