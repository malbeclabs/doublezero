use crate::geoclicommand::GeoCliCommand;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::geolocation::geo_probe::list::ListGeoProbeCommand;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListGeoProbeCliCommand {
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct GeoProbeDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub public_ip: Ipv4Addr,
    pub port: u16,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub exchange: Pubkey,
    pub parent_count: usize,
    pub reference_count: u32,
}

impl ListGeoProbeCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let probes = client.list_geo_probes(ListGeoProbeCommand)?;

        let mut displays: Vec<GeoProbeDisplay> = probes
            .into_iter()
            .map(|(pubkey, probe)| GeoProbeDisplay {
                account: pubkey,
                code: probe.code,
                public_ip: probe.public_ip,
                port: probe.location_offset_port,
                exchange: probe.exchange_pk,
                parent_count: probe.parent_devices.len(),
                reference_count: probe.reference_count,
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
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_cli_geo_probe_list_table() {
        let mut client = MockGeoCliCommand::new();

        let probe1_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");

        let probe1 = GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            exchange_pk,
            public_ip: Ipv4Addr::new(10, 0, 0, 1),
            location_offset_port: 8923,
            code: "ams-probe-01".to_string(),
            parent_devices: vec![],
            metrics_publisher_pk: Pubkey::new_unique(),
            reference_count: 0,
        };

        let mut probes = HashMap::new();
        probes.insert(probe1_pk, probe1);

        client
            .expect_list_geo_probes()
            .returning(move |_| Ok(probes.clone()));

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
    }

    #[test]
    fn test_cli_geo_probe_list_json() {
        let mut client = MockGeoCliCommand::new();

        let probe1_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");

        let probe1 = GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            exchange_pk,
            public_ip: Ipv4Addr::new(10, 0, 0, 1),
            location_offset_port: 8923,
            code: "ams-probe-01".to_string(),
            parent_devices: vec![Pubkey::new_unique()],
            metrics_publisher_pk: Pubkey::new_unique(),
            reference_count: 2,
        };

        let mut probes = HashMap::new();
        probes.insert(probe1_pk, probe1);

        client
            .expect_list_geo_probes()
            .returning(move |_| Ok(probes.clone()));

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
        assert_eq!(parsed[0]["parent_count"], 1);
        assert_eq!(parsed[0]["reference_count"], 2);
    }

    #[test]
    fn test_cli_geo_probe_list_empty() {
        let mut client = MockGeoCliCommand::new();

        client
            .expect_list_geo_probes()
            .returning(|_| Ok(HashMap::new()));

        let mut output = Vec::new();
        let res = ListGeoProbeCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
    }
}
