use crate::{geoclicommand::GeoCliCommand, validators::validate_pubkey_or_code};
use clap::Args;
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
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub exchange: Pubkey,
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
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, probe) = client.get_geo_probe(GetGeoProbeCommand {
            pubkey_or_code: self.probe,
        })?;

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
            exchange: probe.exchange_pk,
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
    use crate::geoclicommand::MockGeoCliCommand;
    use doublezero_geolocation::state::{accounttype::AccountType, geo_probe::GeoProbe};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::net::Ipv4Addr;

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

        let mut output = Vec::new();
        let res = GetGeoProbeCliCommand {
            probe: Pubkey::new_unique().to_string(),
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());

        let mut output = Vec::new();
        let res = GetGeoProbeCliCommand {
            probe: probe_pk.to_string(),
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
        assert!(has_row("account", &probe_pk.to_string()));
        assert!(has_row("code", "ams-probe-01"));
        assert!(has_row("owner", &owner_pk.to_string()));
        assert!(has_row("exchange", &exchange_pk.to_string()));
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

        let mut output = Vec::new();
        let res = GetGeoProbeCliCommand {
            probe: probe_pk.to_string(),
            json: true,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(json["account"].as_str().unwrap(), probe_pk.to_string());
        assert_eq!(json["code"].as_str().unwrap(), "ams-probe-01");
        assert_eq!(json["owner"].as_str().unwrap(), owner_pk.to_string());
        assert_eq!(json["exchange"].as_str().unwrap(), exchange_pk.to_string());
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
}
