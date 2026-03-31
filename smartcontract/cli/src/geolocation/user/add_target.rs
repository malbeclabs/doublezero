use crate::{
    geoclicommand::GeoCliCommand,
    validators::{validate_code, validate_pubkey, validate_pubkey_or_code},
};
use clap::{Args, ValueEnum};
use doublezero_geolocation::state::geolocation_user::GeoLocationTargetType;
use doublezero_sdk::geolocation::{
    geo_probe::{get::GetGeoProbeCommand, list::ListGeoProbeCommand},
    geolocation_user::add_target::AddTargetCommand,
};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};

#[derive(ValueEnum, Debug, Clone)]
pub enum TargetType {
    Outbound,
    Inbound,
    OutboundIcmp,
}

#[derive(Args, Debug)]
#[command(group = clap::ArgGroup::new("probe_source").required(true))]
pub struct AddTargetCliCommand {
    /// User code
    #[arg(long, value_parser = validate_code)]
    pub user: String,
    /// Target type
    #[arg(long = "type", value_enum)]
    pub target_type: TargetType,
    /// Target IPv4 address (required for outbound)
    #[arg(long)]
    pub target_ip: Option<Ipv4Addr>,
    /// Target UDP port for location offsets (outbound only)
    #[arg(long, default_value_t = 8923)]
    pub target_port: u16,
    /// Target signing pubkey (required for inbound)
    #[arg(long, value_parser = validate_pubkey)]
    pub target_pk: Option<String>,
    /// Probe code or pubkey
    #[arg(long, value_parser = validate_pubkey_or_code, group = "probe_source")]
    pub probe: Option<String>,
    /// Exchange code or pubkey (resolves to probe)
    #[arg(long, value_parser = validate_pubkey_or_code, group = "probe_source")]
    pub exchange: Option<String>,
}

impl AddTargetCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (target_type, ip_address, location_offset_port, target_pk) = match self.target_type {
            TargetType::Outbound => {
                let ip = self
                    .target_ip
                    .ok_or_else(|| eyre::eyre!("--target-ip is required for outbound targets"))?;
                (
                    GeoLocationTargetType::Outbound,
                    ip,
                    self.target_port,
                    Pubkey::default(),
                )
            }
            TargetType::Inbound => {
                let pk_str = self
                    .target_pk
                    .ok_or_else(|| eyre::eyre!("--target-pk is required for inbound targets"))?;
                let pk: Pubkey = pk_str.parse().expect("validated by clap");
                (GeoLocationTargetType::Inbound, Ipv4Addr::UNSPECIFIED, 0, pk)
            }
            TargetType::OutboundIcmp => {
                let ip = self.target_ip.ok_or_else(|| {
                    eyre::eyre!("--target-ip is required for outbound-icmp targets")
                })?;
                (
                    GeoLocationTargetType::OutboundIcmp,
                    ip,
                    self.target_port,
                    Pubkey::default(),
                )
            }
        };

        let probe_pk = resolve_probe(client, self.probe, self.exchange)?;

        let sig = client.add_target(AddTargetCommand {
            code: self.user,
            probe_pk,
            target_type,
            ip_address,
            location_offset_port,
            target_pk,
        })?;

        writeln!(out, "Signature: {sig}")?;

        Ok(())
    }
}

pub(super) fn resolve_probe<C: GeoCliCommand>(
    client: &C,
    probe: Option<String>,
    exchange: Option<String>,
) -> eyre::Result<Pubkey> {
    if let Some(probe_id) = probe {
        let (pk, _) = client.get_geo_probe(GetGeoProbeCommand {
            pubkey_or_code: probe_id,
        })?;
        return Ok(pk);
    }

    if let Some(exchange_id) = exchange {
        let exchange_pk = client.resolve_exchange_pk(exchange_id)?;

        let probes = client.list_geo_probes(ListGeoProbeCommand)?;
        let matching: Vec<_> = probes
            .into_iter()
            .filter(|(_, p)| p.exchange_pk == exchange_pk)
            .collect();

        match matching.len() {
            0 => Err(eyre::eyre!("no probe found for exchange {exchange_pk}")),
            1 => Ok(matching.into_iter().next().unwrap().0),
            n => Err(eyre::eyre!(
                "found {n} probes for exchange {exchange_pk}; use --probe to specify which one"
            )),
        }
    } else {
        Err(eyre::eyre!(
            "either --probe or --exchange must be specified"
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geoclicommand::MockGeoCliCommand;
    use doublezero_geolocation::state::{accounttype::AccountType, geo_probe::GeoProbe};
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::collections::HashMap;

    fn make_probe(exchange_pk: Pubkey) -> GeoProbe {
        GeoProbe {
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
        }
    }

    #[test]
    fn test_cli_add_target_outbound_via_probe() {
        let mut client = MockGeoCliCommand::new();

        let probe_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let exchange_pk = Pubkey::new_unique();
        let probe = make_probe(exchange_pk);
        let signature = Signature::new_unique();

        client
            .expect_get_geo_probe()
            .with(predicate::eq(GetGeoProbeCommand {
                pubkey_or_code: "ams-probe-01".to_string(),
            }))
            .returning(move |_| Ok((probe_pk, probe.clone())));

        client
            .expect_add_target()
            .with(predicate::eq(AddTargetCommand {
                code: "geo-user-01".to_string(),
                probe_pk,
                target_type: GeoLocationTargetType::Outbound,
                ip_address: Ipv4Addr::new(8, 8, 8, 8),
                location_offset_port: 8923,
                target_pk: Pubkey::default(),
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = AddTargetCliCommand {
            user: "geo-user-01".to_string(),
            target_type: TargetType::Outbound,
            target_ip: Some(Ipv4Addr::new(8, 8, 8, 8)),
            target_port: 8923,
            target_pk: None,
            probe: Some("ams-probe-01".to_string()),
            exchange: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }

    #[test]
    fn test_cli_add_target_inbound_via_probe() {
        let mut client = MockGeoCliCommand::new();

        let probe_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let exchange_pk = Pubkey::new_unique();
        let probe = make_probe(exchange_pk);
        let target_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let signature = Signature::new_unique();

        client
            .expect_get_geo_probe()
            .with(predicate::eq(GetGeoProbeCommand {
                pubkey_or_code: probe_pk.to_string(),
            }))
            .returning(move |_| Ok((probe_pk, probe.clone())));

        client
            .expect_add_target()
            .with(predicate::eq(AddTargetCommand {
                code: "geo-user-01".to_string(),
                probe_pk,
                target_type: GeoLocationTargetType::Inbound,
                ip_address: Ipv4Addr::UNSPECIFIED,
                location_offset_port: 0,
                target_pk,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = AddTargetCliCommand {
            user: "geo-user-01".to_string(),
            target_type: TargetType::Inbound,
            target_ip: None,
            target_port: 8923,
            target_pk: Some(target_pk.to_string()),
            probe: Some(probe_pk.to_string()),
            exchange: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }

    #[test]
    fn test_cli_add_target_outbound_via_exchange_pubkey() {
        let mut client = MockGeoCliCommand::new();

        let probe_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let probe = make_probe(exchange_pk);
        let signature = Signature::new_unique();

        client
            .expect_resolve_exchange_pk()
            .with(predicate::eq(exchange_pk.to_string()))
            .returning(move |_| Ok(exchange_pk));

        let mut probes = HashMap::new();
        probes.insert(probe_pk, probe);

        client
            .expect_list_geo_probes()
            .returning(move |_| Ok(probes.clone()));

        client
            .expect_add_target()
            .with(predicate::eq(AddTargetCommand {
                code: "geo-user-01".to_string(),
                probe_pk,
                target_type: GeoLocationTargetType::Outbound,
                ip_address: Ipv4Addr::new(8, 8, 8, 8),
                location_offset_port: 8923,
                target_pk: Pubkey::default(),
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = AddTargetCliCommand {
            user: "geo-user-01".to_string(),
            target_type: TargetType::Outbound,
            target_ip: Some(Ipv4Addr::new(8, 8, 8, 8)),
            target_port: 8923,
            target_pk: None,
            probe: None,
            exchange: Some(exchange_pk.to_string()),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }

    #[test]
    fn test_cli_add_target_outbound_via_exchange_code() {
        let mut client = MockGeoCliCommand::new();

        let probe_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let probe = make_probe(exchange_pk);
        let signature = Signature::new_unique();

        client
            .expect_resolve_exchange_pk()
            .with(predicate::eq("xams".to_string()))
            .returning(move |_| Ok(exchange_pk));

        let mut probes = HashMap::new();
        probes.insert(probe_pk, probe);

        client
            .expect_list_geo_probes()
            .returning(move |_| Ok(probes.clone()));

        client
            .expect_add_target()
            .with(predicate::eq(AddTargetCommand {
                code: "geo-user-01".to_string(),
                probe_pk,
                target_type: GeoLocationTargetType::Outbound,
                ip_address: Ipv4Addr::new(8, 8, 8, 8),
                location_offset_port: 8923,
                target_pk: Pubkey::default(),
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = AddTargetCliCommand {
            user: "geo-user-01".to_string(),
            target_type: TargetType::Outbound,
            target_ip: Some(Ipv4Addr::new(8, 8, 8, 8)),
            target_port: 8923,
            target_pk: None,
            probe: None,
            exchange: Some("xams".to_string()),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }

    #[test]
    fn test_cli_add_target_outbound_missing_ip() {
        let mut client = MockGeoCliCommand::new();

        let probe_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let exchange_pk = Pubkey::new_unique();
        let probe = make_probe(exchange_pk);

        client
            .expect_get_geo_probe()
            .returning(move |_| Ok((probe_pk, probe.clone())));

        let mut output = Vec::new();
        let res = AddTargetCliCommand {
            user: "geo-user-01".to_string(),
            target_type: TargetType::Outbound,
            target_ip: None,
            target_port: 8923,
            target_pk: None,
            probe: Some("ams-probe-01".to_string()),
            exchange: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert!(res.unwrap_err().to_string().contains("--target-ip"));
    }

    #[test]
    fn test_cli_add_target_inbound_missing_pk() {
        let client = MockGeoCliCommand::new();

        let mut output = Vec::new();
        let res = AddTargetCliCommand {
            user: "geo-user-01".to_string(),
            target_type: TargetType::Inbound,
            target_ip: None,
            target_port: 8923,
            target_pk: None,
            probe: Some("ams-probe-01".to_string()),
            exchange: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert!(res.unwrap_err().to_string().contains("--target-pk"));
    }

    #[test]
    fn test_cli_add_target_exchange_multiple_probes_errors() {
        let mut client = MockGeoCliCommand::new();

        let probe_pk1 = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let probe_pk2 = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");

        client
            .expect_resolve_exchange_pk()
            .with(predicate::eq(exchange_pk.to_string()))
            .returning(move |_| Ok(exchange_pk));

        let mut probes = HashMap::new();
        probes.insert(probe_pk1, make_probe(exchange_pk));
        probes.insert(probe_pk2, make_probe(exchange_pk));

        client
            .expect_list_geo_probes()
            .returning(move |_| Ok(probes.clone()));

        let mut output = Vec::new();
        let res = AddTargetCliCommand {
            user: "geo-user-01".to_string(),
            target_type: TargetType::Outbound,
            target_ip: Some(Ipv4Addr::new(8, 8, 8, 8)),
            target_port: 8923,
            target_pk: None,
            probe: None,
            exchange: Some(exchange_pk.to_string()),
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        let err = res.unwrap_err().to_string();
        assert!(
            err.contains("found 2 probes"),
            "expected multi-probe error, got: {err}"
        );
        assert!(err.contains("--probe"));
    }
}
