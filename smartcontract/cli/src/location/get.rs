use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_sdk::commands::location::get::GetLocationCommand;
use serde::Serialize;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetLocationCliCommand {
    /// Location Pubkey or code to get details for
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub code: String,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct LocationDisplay {
    pub account: String,
    pub code: String,
    pub name: String,
    pub country: String,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: u32,
    pub reference_count: u32,
    pub status: String,
    pub owner: String,
}

impl GetLocationCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, location) = client.get_location(GetLocationCommand {
            pubkey_or_code: self.code,
        })?;

        let display = LocationDisplay {
            account: pubkey.to_string(),
            code: location.code,
            name: location.name,
            country: location.country,
            lat: location.lat,
            lng: location.lng,
            loc_id: location.loc_id,
            reference_count: location.reference_count,
            status: location.status.to_string(),
            owner: location.owner.to_string(),
        };

        if self.json {
            let json = serde_json::to_string_pretty(&display)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = LocationDisplay::headers();
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
    use crate::{location::get::GetLocationCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{AccountType, GetLocationCommand, Location, LocationStatus};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, str::FromStr};

    #[test]
    fn test_cli_location_get() {
        let mut client = create_test_client();

        let location1_pubkey =
            Pubkey::from_str("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB").unwrap();
        let location1 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            name: "Test Location".to_string(),
            country: "Test Country".to_string(),
            lat: 12.34,
            lng: 56.78,
            loc_id: 1,
            status: LocationStatus::Activated,
            owner: location1_pubkey,
        };

        let location2 = location1.clone();
        client
            .expect_get_location()
            .with(predicate::eq(GetLocationCommand {
                pubkey_or_code: location1_pubkey.to_string(),
            }))
            .returning(move |_| Ok((location1_pubkey, location2.clone())));
        let location3 = location1.clone();
        client
            .expect_get_location()
            .with(predicate::eq(GetLocationCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((location1_pubkey, location3.clone())));
        client
            .expect_get_location()
            .returning(move |_| Err(eyre::eyre!("not found")));

        client.expect_list_location().returning(move |_| {
            let mut list = HashMap::new();
            list.insert(location1_pubkey, location1.clone());
            Ok(list)
        });

        // Expected failure
        let mut output = Vec::new();
        let res = GetLocationCliCommand {
            code: Pubkey::new_unique().to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success by pubkey (table)
        let mut output = Vec::new();
        let res = GetLocationCliCommand {
            code: location1_pubkey.to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("account", &location1_pubkey.to_string()),
            "account row should contain pubkey"
        );
        assert!(has_row("code", "test"), "code row should contain value");
        assert!(
            has_row("status", "activated"),
            "status row should contain value"
        );

        // Expected success by code (JSON)
        let mut output = Vec::new();
        let res = GetLocationCliCommand {
            code: "test".to_string(),
            json: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(
            json["account"].as_str().unwrap(),
            location1_pubkey.to_string()
        );
        assert_eq!(json["code"].as_str().unwrap(), "test");
        assert_eq!(json["name"].as_str().unwrap(), "Test Location");
        assert_eq!(json["country"].as_str().unwrap(), "Test Country");
        assert_eq!(json["status"].as_str().unwrap(), "activated");
        assert_eq!(json["lat"].as_f64().unwrap(), 12.34);
        assert_eq!(json["lng"].as_f64().unwrap(), 56.78);
        assert_eq!(json["loc_id"].as_u64().unwrap(), 1);
        assert_eq!(json["reference_count"].as_u64().unwrap(), 0);
    }
}
