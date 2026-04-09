use crate::{geoclicommand::GeoCliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_geolocation::state::geolocation_user::{
    GeoLocationTargetType, GeolocationBillingConfig,
};
use doublezero_program_common::serializer;
use doublezero_sdk::geolocation::geolocation_user::get::GetGeolocationUserCommand;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct GetGeolocationUserCliCommand {
    /// User pubkey or code to retrieve
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub user: String,
    /// Output as pretty JSON
    #[arg(long, default_value_t = false, conflicts_with = "json_compact")]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false, conflicts_with = "json")]
    pub json_compact: bool,
}

#[derive(Serialize)]
struct GeolocationUserGetDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub token_account: Pubkey,
    pub status: String,
    pub payment_status: String,
    pub billing: String,
    pub target_count: usize,
    pub targets: Vec<TargetDisplay>,
}

#[derive(Tabled, Serialize)]
struct TargetDisplay {
    #[serde(rename = "type")]
    #[tabled(rename = "type")]
    pub target_type: String,
    pub ip: String,
    pub port: u16,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub target_signing_pubkey: Pubkey,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(rename = "probe")]
    pub geoprobe_pk: Pubkey,
}

impl GetGeolocationUserCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, user) = client.get_geolocation_user(GetGeolocationUserCommand {
            pubkey_or_code: self.user,
        })?;

        let targets: Vec<TargetDisplay> = user
            .targets
            .iter()
            .map(|t| {
                let (ip, port) = match t.target_type {
                    GeoLocationTargetType::Outbound | GeoLocationTargetType::OutboundIcmp => {
                        (t.ip_address.to_string(), t.location_offset_port)
                    }
                    GeoLocationTargetType::Inbound => ("-".to_string(), 0),
                };
                TargetDisplay {
                    target_type: t.target_type.to_string(),
                    ip,
                    port,
                    target_signing_pubkey: t.target_pk,
                    geoprobe_pk: t.geoprobe_pk,
                }
            })
            .collect();

        let billing_str = match user.billing {
            GeolocationBillingConfig::FlatPerEpoch(cfg) => {
                format!(
                    "flat_per_epoch(rate: {}, last_epoch: {})",
                    cfg.rate, cfg.last_deduction_dz_epoch
                )
            }
        };

        let display = GeolocationUserGetDisplay {
            account: pubkey,
            code: user.code,
            owner: user.owner,
            token_account: user.token_account,
            status: user.status.to_string(),
            payment_status: user.payment_status.to_string(),
            billing: billing_str,
            target_count: targets.len(),
            targets,
        };

        if self.json || self.json_compact {
            let json = if self.json_compact {
                serde_json::to_string(&display)?
            } else {
                serde_json::to_string_pretty(&display)?
            };
            writeln!(out, "{json}")?;
        } else {
            let rows: Vec<(&str, String)> = vec![
                ("account", display.account.to_string()),
                ("code", display.code.clone()),
                ("owner", display.owner.to_string()),
                ("token_account", display.token_account.to_string()),
                ("status", display.status.clone()),
                ("payment_status", display.payment_status.clone()),
                ("billing", display.billing.clone()),
                ("target_count", display.target_count.to_string()),
            ];
            let max_len = rows.iter().map(|(h, _)| h.len()).max().unwrap_or(0);
            for (header, value) in &rows {
                writeln!(out, " {header:<max_len$} | {value}")?;
            }

            if !display.targets.is_empty() {
                writeln!(out)?;
                writeln!(out, "Targets:")?;
                let table = Table::new(&display.targets)
                    .with(Style::psql().remove_horizontals())
                    .to_string();
                writeln!(out, "{table}")?;
            }
        }

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
            FlatPerEpochConfig, GeoLocationTargetType, GeolocationPaymentStatus, GeolocationTarget,
            GeolocationUser, GeolocationUserStatus,
        },
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::net::Ipv4Addr;

    fn make_user(code: &str, targets: Vec<GeolocationTarget>) -> GeolocationUser {
        GeolocationUser {
            account_type: AccountType::GeolocationUser,
            owner: Pubkey::from_str_const("DDddB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt"),
            code: code.to_string(),
            token_account: Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc"),
            payment_status: GeolocationPaymentStatus::Paid,
            billing: GeolocationBillingConfig::FlatPerEpoch(FlatPerEpochConfig {
                rate: 1000,
                last_deduction_dz_epoch: 42,
            }),
            status: GeolocationUserStatus::Activated,
            targets,
        }
    }

    #[test]
    fn test_cli_geolocation_user_get() {
        let mut client = MockGeoCliCommand::new();
        let user_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let probe_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");

        let user = make_user(
            "geo-user-01",
            vec![GeolocationTarget {
                target_type: GeoLocationTargetType::Outbound,
                ip_address: Ipv4Addr::new(8, 8, 8, 8),
                location_offset_port: 8923,
                target_pk: Pubkey::default(),
                geoprobe_pk: probe_pk,
            }],
        );

        client
            .expect_get_geolocation_user()
            .with(predicate::eq(GetGeolocationUserCommand {
                pubkey_or_code: user_pk.to_string(),
            }))
            .returning(move |_| Ok((user_pk, user.clone())));

        let mut output = Vec::new();
        let res = GetGeolocationUserCliCommand {
            user: user_pk.to_string(),
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(has_row("account", &user_pk.to_string()));
        assert!(has_row("code", "geo-user-01"));
        assert!(has_row("status", "activated"));
        assert!(has_row("payment_status", "paid"));
        assert!(has_row("target_count", "1"));
        assert!(output_str.contains("Targets:"));
        assert!(output_str.contains("8.8.8.8"));
    }

    #[test]
    fn test_cli_geolocation_user_get_json() {
        let mut client = MockGeoCliCommand::new();
        let user_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");

        let user = make_user("geo-user-01", vec![]);

        client
            .expect_get_geolocation_user()
            .with(predicate::eq(GetGeolocationUserCommand {
                pubkey_or_code: user_pk.to_string(),
            }))
            .returning(move |_| Ok((user_pk, user.clone())));

        let mut output = Vec::new();
        let res = GetGeolocationUserCliCommand {
            user: user_pk.to_string(),
            json: true,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(json["account"].as_str().unwrap(), user_pk.to_string());
        assert_eq!(json["code"].as_str().unwrap(), "geo-user-01");
        assert_eq!(json["status"].as_str().unwrap(), "activated");
        assert_eq!(json["payment_status"].as_str().unwrap(), "paid");
        assert_eq!(json["target_count"].as_u64().unwrap(), 0);
        assert!(json["targets"].as_array().unwrap().is_empty());
    }

    #[test]
    fn test_cli_geolocation_user_get_no_targets_section() {
        let mut client = MockGeoCliCommand::new();
        let user_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");

        let user = make_user("geo-user-01", vec![]);

        client
            .expect_get_geolocation_user()
            .with(predicate::eq(GetGeolocationUserCommand {
                pubkey_or_code: user_pk.to_string(),
            }))
            .returning(move |_| Ok((user_pk, user.clone())));

        let mut output = Vec::new();
        let res = GetGeolocationUserCliCommand {
            user: user_pk.to_string(),
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(!output_str.contains("Targets:"));
    }

    #[test]
    fn test_cli_geolocation_user_get_json_compact() {
        let mut client = MockGeoCliCommand::new();
        let user_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let probe_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");

        let user = make_user(
            "geo-user-01",
            vec![GeolocationTarget {
                target_type: GeoLocationTargetType::Outbound,
                ip_address: Ipv4Addr::new(8, 8, 8, 8),
                location_offset_port: 8923,
                target_pk: Pubkey::default(),
                geoprobe_pk: probe_pk,
            }],
        );

        client
            .expect_get_geolocation_user()
            .with(predicate::eq(GetGeolocationUserCommand {
                pubkey_or_code: user_pk.to_string(),
            }))
            .returning(move |_| Ok((user_pk, user.clone())));

        let mut output = Vec::new();
        let res = GetGeolocationUserCliCommand {
            user: user_pk.to_string(),
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let trimmed = output_str.trim();
        assert!(!trimmed.contains('\n'), "compact JSON must be single-line");
        let json: serde_json::Value = serde_json::from_str(trimmed).unwrap();
        assert_eq!(json["account"].as_str().unwrap(), user_pk.to_string());
        assert_eq!(json["code"].as_str().unwrap(), "geo-user-01");
        assert_eq!(json["target_count"].as_u64().unwrap(), 1);
        assert_eq!(json["targets"].as_array().unwrap().len(), 1);
        assert_eq!(json["targets"][0]["ip"].as_str().unwrap(), "8.8.8.8");
    }
}
