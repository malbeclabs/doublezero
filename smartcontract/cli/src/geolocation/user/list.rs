use crate::geoclicommand::GeoCliCommand;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::geolocation::geolocation_user::list::ListGeolocationUserCommand;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListGeolocationUserCliCommand {
    /// Output as pretty JSON
    #[arg(long, default_value_t = false, conflicts_with = "json_compact")]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false, conflicts_with = "json")]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct GeolocationUserDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
    pub status: String,
    pub payment_status: String,
    pub target_count: usize,
}

impl ListGeolocationUserCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let users = client.list_geolocation_users(ListGeolocationUserCommand)?;

        let mut displays: Vec<GeolocationUserDisplay> = users
            .into_iter()
            .map(|(pubkey, user)| GeolocationUserDisplay {
                account: pubkey,
                code: user.code,
                owner: user.owner,
                status: user.status.to_string(),
                payment_status: user.payment_status.to_string(),
                target_count: user.targets.len(),
            })
            .collect();

        displays.sort_by(|a, b| a.code.cmp(&b.code));

        let res = if self.json {
            serde_json::to_string_pretty(&displays)?
        } else if self.json_compact {
            serde_json::to_string(&displays)?
        } else {
            Table::new(displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geoclicommand::MockGeoCliCommand;
    use doublezero_geolocation::state::{
        accounttype::AccountType,
        geolocation_user::{
            GeolocationBillingConfig, GeolocationPaymentStatus, GeolocationUser,
            GeolocationUserStatus,
        },
    };
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    fn make_user(code: &str) -> GeolocationUser {
        GeolocationUser {
            account_type: AccountType::GeolocationUser,
            owner: Pubkey::new_unique(),
            code: code.to_string(),
            token_account: Pubkey::new_unique(),
            payment_status: GeolocationPaymentStatus::Delinquent,
            billing: GeolocationBillingConfig::default(),
            status: GeolocationUserStatus::Activated,
            targets: vec![],
        }
    }

    #[test]
    fn test_cli_geolocation_user_list_table() {
        let mut client = MockGeoCliCommand::new();

        let user1_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");

        let mut users = HashMap::new();
        users.insert(user1_pk, make_user("geo-user-01"));

        client
            .expect_list_geolocation_users()
            .returning(move |_| Ok(users.clone()));

        let mut output = Vec::new();
        let res = ListGeolocationUserCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("geo-user-01"));
        assert!(output_str.contains("activated"));
        assert!(output_str.contains("delinquent"));
    }

    #[test]
    fn test_cli_geolocation_user_list_json() {
        let mut client = MockGeoCliCommand::new();

        let user1_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");

        let mut users = HashMap::new();
        users.insert(user1_pk, make_user("geo-user-01"));

        client
            .expect_list_geolocation_users()
            .returning(move |_| Ok(users.clone()));

        let mut output = Vec::new();
        let res = ListGeolocationUserCliCommand {
            json: true,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let parsed: Vec<serde_json::Value> = serde_json::from_str(output_str.trim()).unwrap();
        assert_eq!(parsed.len(), 1);
        assert_eq!(parsed[0]["code"], "geo-user-01");
        assert_eq!(parsed[0]["target_count"], 0);
    }

    #[test]
    fn test_cli_geolocation_user_list_empty() {
        let mut client = MockGeoCliCommand::new();

        client
            .expect_list_geolocation_users()
            .returning(|_| Ok(HashMap::new()));

        let mut output = Vec::new();
        let res = ListGeolocationUserCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
    }
}
