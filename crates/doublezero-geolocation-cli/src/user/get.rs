use crate::client::GeoCliCommand;
use clap::Args;
use doublezero_cli_core::{validators::validate_pubkey_or_code, CliContext};
use doublezero_geolocation::state::geolocation_user::{
    GeoLocationTargetType, GeolocationBillingConfig,
};
use doublezero_program_common::serializer;
use doublezero_sdk::geolocation::{
    geo_probe::list::ListGeoProbeCommand, geolocation_user::get::GetGeolocationUserCommand,
};
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
    pub result_destination: String,
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
    #[tabled(rename = "probe")]
    pub probe: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub geoprobe_pk: Pubkey,
}

impl GetGeolocationUserCliCommand {
    pub async fn execute<C: GeoCliCommand, W: Write>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        tracing::debug!(env = %ctx.env, user = %self.user, "geolocation user get");

        let (pubkey, user) = client.get_geolocation_user(GetGeolocationUserCommand {
            pubkey_or_code: self.user,
        })?;

        let probes = client.list_geo_probes(ListGeoProbeCommand)?;

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
                let probe = probes
                    .get(&t.geoprobe_pk)
                    .map(|p| p.code.clone())
                    .unwrap_or_else(|| t.geoprobe_pk.to_string());
                TargetDisplay {
                    target_type: t.target_type.to_string(),
                    ip,
                    port,
                    target_signing_pubkey: t.target_pk,
                    probe,
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

        let result_dest_str = if user.result_destination.is_empty() {
            "none".to_string()
        } else {
            user.result_destination.clone()
        };

        let display = GeolocationUserGetDisplay {
            account: pubkey,
            code: user.code,
            owner: user.owner,
            token_account: user.token_account,
            status: user.status.to_string(),
            payment_status: user.payment_status.to_string(),
            billing: billing_str,
            result_destination: result_dest_str,
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
                ("result_destination", display.result_destination.clone()),
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
    use crate::client::MockGeoCliCommand;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_geolocation::state::{
        accounttype::AccountType,
        geo_probe::GeoProbe,
        geolocation_user::{
            FlatPerEpochConfig, GeoLocationTargetType, GeolocationPaymentStatus, GeolocationTarget,
            GeolocationUser, GeolocationUserStatus,
        },
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, net::Ipv4Addr};

    fn make_probes(probe_pk: Pubkey, code: &str) -> HashMap<Pubkey, GeoProbe> {
        let mut probes = HashMap::new();
        probes.insert(
            probe_pk,
            GeoProbe {
                account_type: AccountType::GeoProbe,
                owner: Pubkey::default(),
                exchange_pk: Pubkey::default(),
                public_ip: Ipv4Addr::new(10, 0, 0, 1),
                location_offset_port: 8923,
                code: code.to_string(),
                parent_devices: vec![],
                metrics_publisher_pk: Pubkey::default(),
                reference_count: 0,
                target_update_count: 0,
            },
        );
        probes
    }

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
            result_destination: String::new(),
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

        let probes = make_probes(probe_pk, "ams-probe-01");
        client
            .expect_list_geo_probes()
            .returning(move |_| Ok(probes.clone()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetGeolocationUserCliCommand {
                user: user_pk.to_string(),
                json: false,
                json_compact: false,
            }
            .execute(&ctx, &client, &mut output),
        );
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
        assert!(has_row("result_destination", "none"));
        assert!(has_row("target_count", "1"));
        assert!(output_str.contains("Targets:"));
        assert!(output_str.contains("8.8.8.8"));
        // probe column shows the code, not the pubkey
        assert!(output_str.contains("ams-probe-01"));
        assert!(!output_str.contains(&probe_pk.to_string()));
    }

    #[test]
    fn test_cli_geolocation_user_get_unknown_probe_falls_back_to_pubkey() {
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

        client
            .expect_list_geo_probes()
            .returning(|_| Ok(HashMap::new()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetGeolocationUserCliCommand {
                user: user_pk.to_string(),
                json: false,
                json_compact: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains(&probe_pk.to_string()));
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

        client
            .expect_list_geo_probes()
            .returning(|_| Ok(HashMap::new()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetGeolocationUserCliCommand {
                user: user_pk.to_string(),
                json: true,
                json_compact: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(json["account"].as_str().unwrap(), user_pk.to_string());
        assert_eq!(json["code"].as_str().unwrap(), "geo-user-01");
        assert_eq!(json["status"].as_str().unwrap(), "activated");
        assert_eq!(json["payment_status"].as_str().unwrap(), "paid");
        assert_eq!(json["result_destination"].as_str().unwrap(), "none");
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

        client
            .expect_list_geo_probes()
            .returning(|_| Ok(HashMap::new()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetGeolocationUserCliCommand {
                user: user_pk.to_string(),
                json: false,
                json_compact: false,
            }
            .execute(&ctx, &client, &mut output),
        );
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

        let probes = make_probes(probe_pk, "ams-probe-01");
        client
            .expect_list_geo_probes()
            .returning(move |_| Ok(probes.clone()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetGeolocationUserCliCommand {
                user: user_pk.to_string(),
                json: false,
                json_compact: true,
            }
            .execute(&ctx, &client, &mut output),
        );
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
        // JSON carries the resolved probe code plus the canonical pubkey
        assert_eq!(
            json["targets"][0]["probe"].as_str().unwrap(),
            "ams-probe-01"
        );
        assert_eq!(
            json["targets"][0]["geoprobe_pk"].as_str().unwrap(),
            probe_pk.to_string()
        );
    }
}
