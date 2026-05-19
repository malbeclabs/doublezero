use crate::geoclicommand::GeoCliCommand;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::geolocation::geo_probe::list::ListGeoProbeCommand;
use serde::{Serialize, Serializer};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};
use tabled::{settings::Style, Table, Tabled};

fn serialize_pubkey_vec_as_string_array<S>(pks: &[Pubkey], s: S) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    let strs: Vec<String> = pks.iter().map(|pk| pk.to_string()).collect();
    strs.serialize(s)
}

#[derive(Args, Debug)]
pub struct ListGeoProbeCliCommand {
    /// Output as pretty JSON
    #[arg(long, default_value_t = false, conflicts_with = "json_compact")]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false, conflicts_with = "json")]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct GeoProbeDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub public_ip: Ipv4Addr,
    pub port: u16,
    pub exchange: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub signing_pubkey: Pubkey,
    #[serde(serialize_with = "serialize_pubkey_vec_as_string_array")]
    #[tabled(skip)]
    pub parent_devices: Vec<Pubkey>,
    pub parent_count: usize,
    pub reference_count: u32,
}

impl ListGeoProbeCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let probes = client.list_geo_probes(ListGeoProbeCommand)?;
        let exchanges = client.list_exchanges()?;

        let mut displays: Vec<GeoProbeDisplay> = probes
            .into_iter()
            .map(|(pubkey, probe)| {
                let parent_count = probe.parent_devices.len();
                let exchange = exchanges
                    .get(&probe.exchange_pk)
                    .map(|ex| ex.code.clone())
                    .unwrap_or_else(|| probe.exchange_pk.to_string());
                GeoProbeDisplay {
                    account: pubkey,
                    code: probe.code,
                    public_ip: probe.public_ip,
                    port: probe.location_offset_port,
                    exchange,
                    signing_pubkey: probe.metrics_publisher_pk,
                    parent_devices: probe.parent_devices,
                    parent_count,
                    reference_count: probe.reference_count,
                }
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
    use doublezero_geolocation::state::{accounttype::AccountType, geo_probe::GeoProbe};
    use doublezero_sdk::{AccountType as SvcAccountType, Exchange, ExchangeStatus};
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    fn make_exchange(pk: Pubkey, code: &str) -> Exchange {
        Exchange {
            account_type: SvcAccountType::Exchange,
            owner: pk,
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
        }
    }

    #[test]
    fn test_cli_geo_probe_list_table() {
        let mut client = MockGeoCliCommand::new();

        let probe1_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let signing_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");

        let probe1 = GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: Pubkey::new_unique(),
            exchange_pk,
            public_ip: Ipv4Addr::new(10, 0, 0, 1),
            location_offset_port: 8923,
            code: "ams-probe-01".to_string(),
            parent_devices: vec![],
            metrics_publisher_pk: signing_pk,
            reference_count: 0,
            target_update_count: 0,
        };

        let mut probes = HashMap::new();
        probes.insert(probe1_pk, probe1);

        client
            .expect_list_geo_probes()
            .returning(move |_| Ok(probes.clone()));

        let mut exchanges = HashMap::new();
        exchanges.insert(exchange_pk, make_exchange(exchange_pk, "ams"));
        client
            .expect_list_exchanges()
            .returning(move || Ok(exchanges.clone()));

        let mut output = Vec::new();
        let res = ListGeoProbeCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("ams-probe-01"));
        assert!(output_str.contains("10.0.0.1"));
        assert!(output_str.contains("ams"));
        assert!(!output_str.contains(&exchange_pk.to_string()));
        assert!(output_str.contains(&signing_pk.to_string()));
    }

    #[test]
    fn test_cli_geo_probe_list_table_unknown_exchange_falls_back_to_pubkey() {
        let mut client = MockGeoCliCommand::new();

        let probe1_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");

        let probe1 = GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: Pubkey::new_unique(),
            exchange_pk,
            public_ip: Ipv4Addr::new(10, 0, 0, 1),
            location_offset_port: 8923,
            code: "ams-probe-01".to_string(),
            parent_devices: vec![],
            metrics_publisher_pk: Pubkey::new_unique(),
            reference_count: 0,
            target_update_count: 0,
        };

        let mut probes = HashMap::new();
        probes.insert(probe1_pk, probe1);

        client
            .expect_list_geo_probes()
            .returning(move |_| Ok(probes.clone()));
        client
            .expect_list_exchanges()
            .returning(|| Ok(HashMap::new()));

        let mut output = Vec::new();
        let res = ListGeoProbeCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains(&exchange_pk.to_string()));
    }

    #[test]
    fn test_cli_geo_probe_list_json() {
        let mut client = MockGeoCliCommand::new();

        let probe1_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let parent_pk = Pubkey::from_str_const("AQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let signing_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");

        let probe1 = GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: Pubkey::new_unique(),
            exchange_pk,
            public_ip: Ipv4Addr::new(10, 0, 0, 1),
            location_offset_port: 8923,
            code: "ams-probe-01".to_string(),
            parent_devices: vec![parent_pk],
            metrics_publisher_pk: signing_pk,
            reference_count: 2,
            target_update_count: 0,
        };

        let mut probes = HashMap::new();
        probes.insert(probe1_pk, probe1);

        client
            .expect_list_geo_probes()
            .returning(move |_| Ok(probes.clone()));

        let mut exchanges = HashMap::new();
        exchanges.insert(exchange_pk, make_exchange(exchange_pk, "ams"));
        client
            .expect_list_exchanges()
            .returning(move || Ok(exchanges.clone()));

        let mut output = Vec::new();
        let res = ListGeoProbeCliCommand {
            json: true,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let parsed: Vec<serde_json::Value> = serde_json::from_str(output_str.trim()).unwrap();
        assert_eq!(parsed.len(), 1);
        assert_eq!(parsed[0]["code"], "ams-probe-01");
        assert_eq!(parsed[0]["exchange"], "ams");
        assert_eq!(parsed[0]["signing_pubkey"], signing_pk.to_string());
        let parents = parsed[0]["parent_devices"].as_array().unwrap();
        assert_eq!(parents.len(), 1);
        assert_eq!(parents[0].as_str().unwrap(), parent_pk.to_string());
        assert_eq!(parsed[0]["reference_count"], 2);
    }

    #[test]
    fn test_cli_geo_probe_list_empty() {
        let mut client = MockGeoCliCommand::new();

        client
            .expect_list_geo_probes()
            .returning(|_| Ok(HashMap::new()));
        client
            .expect_list_exchanges()
            .returning(|| Ok(HashMap::new()));

        let mut output = Vec::new();
        let res = ListGeoProbeCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
    }
}
