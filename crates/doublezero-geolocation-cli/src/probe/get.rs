use crate::client::GeoCliCommand;
use clap::Args;
use doublezero_cli_core::{validators::validate_pubkey_or_code, CliContext};
use doublezero_program_common::serializer;
use doublezero_sdk::geolocation::geo_probe::get::GetGeoProbeCommand;
use serde::{Serialize, Serializer};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};
use tabled::Tabled;

fn serialize_pubkey_vec_as_string_array<S>(pks: &[Pubkey], s: S) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    let strs: Vec<String> = pks.iter().map(|pk| pk.to_string()).collect();
    strs.serialize(s)
}

#[derive(Args, Debug)]
pub struct GetGeoProbeCliCommand {
    /// Probe pubkey or code to retrieve
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub probe: String,
    /// Output as pretty JSON
    #[arg(long, default_value_t = false, conflicts_with = "json_compact")]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false, conflicts_with = "json")]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
struct GeoProbeGetDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
    pub exchange: String,
    pub public_ip: Ipv4Addr,
    pub port: u16,
    #[serde(serialize_with = "serialize_pubkey_vec_as_string_array")]
    #[tabled(skip)]
    pub parent_devices: Vec<Pubkey>,
    #[serde(skip)]
    #[tabled(rename = "parent_devices")]
    pub parent_devices_display: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub signing_pubkey: Pubkey,
    pub reference_count: u32,
}

impl GetGeoProbeCliCommand {
    pub async fn execute<C: GeoCliCommand, W: Write>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        tracing::debug!(env = %ctx.env, probe = %self.probe, "geolocation probe get");

        let (pubkey, probe) = client.get_geo_probe(GetGeoProbeCommand {
            pubkey_or_code: self.probe,
        })?;

        let exchanges = client.list_exchanges()?;
        let exchange = exchanges
            .get(&probe.exchange_pk)
            .map(|ex| ex.code.clone())
            .unwrap_or_else(|| probe.exchange_pk.to_string());

        let parent_devices_display = format!(
            "[{}]",
            probe
                .parent_devices
                .iter()
                .map(|pk| pk.to_string())
                .collect::<Vec<_>>()
                .join(", ")
        );
        let display = GeoProbeGetDisplay {
            account: pubkey,
            code: probe.code,
            owner: probe.owner,
            exchange,
            public_ip: probe.public_ip,
            port: probe.location_offset_port,
            parent_devices: probe.parent_devices,
            parent_devices_display,
            signing_pubkey: probe.metrics_publisher_pk,
            reference_count: probe.reference_count,
        };

        if self.json || self.json_compact {
            let json = if self.json_compact {
                serde_json::to_string(&display)?
            } else {
                serde_json::to_string_pretty(&display)?
            };
            writeln!(out, "{json}")?;
        } else {
            let headers = GeoProbeGetDisplay::headers();
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
    use super::*;
    use crate::client::MockGeoCliCommand;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_geolocation::state::{accounttype::AccountType, geo_probe::GeoProbe};
    use doublezero_sdk::{AccountType as SvcAccountType, Exchange, ExchangeStatus};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, net::Ipv4Addr};

    fn make_exchanges(exchange_pk: Pubkey, code: &str) -> HashMap<Pubkey, Exchange> {
        let mut exchanges = HashMap::new();
        exchanges.insert(
            exchange_pk,
            Exchange {
                account_type: SvcAccountType::Exchange,
                owner: exchange_pk,
                index: 0,
                bump_seed: 0,
                reference_count: 0,
                device1_pk: Pubkey::default(),
                device2_pk: Pubkey::default(),
                lat: 0.0,
                lng: 0.0,
                bgp_community: 0,
                unused: 0,
                status: ExchangeStatus::Activated,
                code: code.to_string(),
                name: code.to_string(),
            },
        );
        exchanges
    }

    fn setup_client() -> (MockGeoCliCommand, Pubkey, Pubkey, Pubkey, Pubkey) {
        let client = MockGeoCliCommand::new();
        let probe_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let owner_pk = Pubkey::from_str_const("DDddB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let metrics_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        (client, probe_pk, owner_pk, exchange_pk, metrics_pk)
    }

    fn make_probe(
        owner_pk: Pubkey,
        exchange_pk: Pubkey,
        metrics_pk: Pubkey,
        parent_devices: Vec<Pubkey>,
    ) -> GeoProbe {
        GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: owner_pk,
            exchange_pk,
            public_ip: Ipv4Addr::new(10, 0, 0, 1),
            location_offset_port: 8923,
            code: "ams-probe-01".to_string(),
            parent_devices,
            metrics_publisher_pk: metrics_pk,
            reference_count: 0,
            target_update_count: 0,
        }
    }

    #[test]
    fn test_cli_geo_probe_get() {
        let (mut client, probe_pk, owner_pk, exchange_pk, metrics_pk) = setup_client();
        let probe = make_probe(owner_pk, exchange_pk, metrics_pk, vec![]);

        client
            .expect_get_geo_probe()
            .with(predicate::eq(GetGeoProbeCommand {
                pubkey_or_code: probe_pk.to_string(),
            }))
            .returning(move |_| Ok((probe_pk, probe.clone())));

        client
            .expect_get_geo_probe()
            .returning(move |_| Err(eyre::eyre!("not found")));

        let exchanges = make_exchanges(exchange_pk, "ams");
        client
            .expect_list_exchanges()
            .returning(move || Ok(exchanges.clone()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetGeoProbeCliCommand {
                probe: Pubkey::new_unique().to_string(),
                json: false,
                json_compact: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_err());

        let mut output = Vec::new();
        let res = block_on(
            GetGeoProbeCliCommand {
                probe: probe_pk.to_string(),
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
        assert!(has_row("account", &probe_pk.to_string()));
        assert!(has_row("code", "ams-probe-01"));
        assert!(has_row("owner", &owner_pk.to_string()));
        // exchange column shows the code, not the pubkey
        assert!(has_row("exchange", "ams"));
        assert!(!output_str.contains(&exchange_pk.to_string()));
        assert!(has_row("public_ip", "10.0.0.1"));
        assert!(has_row("port", "8923"));
        assert!(has_row("signing_pubkey", &metrics_pk.to_string()));
        assert!(has_row("reference_count", "0"));
    }

    #[test]
    fn test_cli_geo_probe_get_json() {
        let (mut client, probe_pk, owner_pk, exchange_pk, metrics_pk) = setup_client();
        let parent_pk = Pubkey::from_str_const("AQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let probe = make_probe(owner_pk, exchange_pk, metrics_pk, vec![parent_pk]);

        client
            .expect_get_geo_probe()
            .with(predicate::eq(GetGeoProbeCommand {
                pubkey_or_code: probe_pk.to_string(),
            }))
            .returning(move |_| Ok((probe_pk, probe.clone())));

        let exchanges = make_exchanges(exchange_pk, "ams");
        client
            .expect_list_exchanges()
            .returning(move || Ok(exchanges.clone()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetGeoProbeCliCommand {
                probe: probe_pk.to_string(),
                json: true,
                json_compact: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(json["account"].as_str().unwrap(), probe_pk.to_string());
        assert_eq!(json["code"].as_str().unwrap(), "ams-probe-01");
        assert_eq!(json["owner"].as_str().unwrap(), owner_pk.to_string());
        assert_eq!(json["exchange"].as_str().unwrap(), "ams");
        assert_eq!(json["public_ip"].as_str().unwrap(), "10.0.0.1");
        assert_eq!(json["port"].as_u64().unwrap(), 8923);
        let parents = json["parent_devices"].as_array().unwrap();
        assert_eq!(parents.len(), 1);
        assert_eq!(parents[0].as_str().unwrap(), parent_pk.to_string());
        assert_eq!(
            json["signing_pubkey"].as_str().unwrap(),
            metrics_pk.to_string()
        );
        assert_eq!(json["reference_count"].as_u64().unwrap(), 0);
    }

    #[test]
    fn test_cli_geo_probe_get_json_compact() {
        let (mut client, probe_pk, owner_pk, exchange_pk, metrics_pk) = setup_client();
        let parent_pk = Pubkey::from_str_const("AQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let probe = make_probe(owner_pk, exchange_pk, metrics_pk, vec![parent_pk]);

        client
            .expect_get_geo_probe()
            .with(predicate::eq(GetGeoProbeCommand {
                pubkey_or_code: probe_pk.to_string(),
            }))
            .returning(move |_| Ok((probe_pk, probe.clone())));

        let exchanges = make_exchanges(exchange_pk, "ams");
        client
            .expect_list_exchanges()
            .returning(move || Ok(exchanges.clone()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetGeoProbeCliCommand {
                probe: probe_pk.to_string(),
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
        assert_eq!(json["account"].as_str().unwrap(), probe_pk.to_string());
        assert_eq!(json["code"].as_str().unwrap(), "ams-probe-01");
        assert_eq!(json["exchange"].as_str().unwrap(), "ams");
        assert_eq!(json["parent_devices"].as_array().unwrap().len(), 1);
    }

    #[test]
    fn test_cli_geo_probe_get_unknown_exchange_falls_back_to_pubkey() {
        let (mut client, probe_pk, owner_pk, exchange_pk, metrics_pk) = setup_client();
        let probe = make_probe(owner_pk, exchange_pk, metrics_pk, vec![]);

        client
            .expect_get_geo_probe()
            .with(predicate::eq(GetGeoProbeCommand {
                pubkey_or_code: probe_pk.to_string(),
            }))
            .returning(move |_| Ok((probe_pk, probe.clone())));

        client
            .expect_list_exchanges()
            .returning(|| Ok(HashMap::new()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetGeoProbeCliCommand {
                probe: probe_pk.to_string(),
                json: false,
                json_compact: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains(&exchange_pk.to_string()));
    }
}
