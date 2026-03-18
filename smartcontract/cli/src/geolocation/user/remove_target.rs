use crate::{
    geoclicommand::GeoCliCommand,
    validators::{validate_code, validate_pubkey, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_geolocation::state::geolocation_user::GeoLocationTargetType;
use doublezero_sdk::geolocation::geolocation_user::remove_target::RemoveTargetCommand;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};

use super::add_target::TargetType;

#[derive(Args, Debug)]
#[command(group = clap::ArgGroup::new("probe_source").required(true))]
pub struct RemoveTargetCliCommand {
    /// User code
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Target type
    #[arg(long = "type", value_enum)]
    pub target_type: TargetType,
    /// Target IPv4 address (required for outbound)
    #[arg(long)]
    pub target_ip: Option<Ipv4Addr>,
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

impl RemoveTargetCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (target_type, ip_address, target_pk) = match self.target_type {
            TargetType::Outbound => {
                let ip = self
                    .target_ip
                    .ok_or_else(|| eyre::eyre!("--target-ip is required for outbound targets"))?;
                (GeoLocationTargetType::Outbound, ip, Pubkey::default())
            }
            TargetType::Inbound => {
                let pk_str = self
                    .target_pk
                    .ok_or_else(|| eyre::eyre!("--target-pk is required for inbound targets"))?;
                let pk: Pubkey = pk_str.parse().expect("validated by clap");
                (GeoLocationTargetType::Inbound, Ipv4Addr::UNSPECIFIED, pk)
            }
        };

        let probe_pk = super::add_target::resolve_probe(client, self.probe, self.exchange)?;

        let sig = client.remove_target(RemoveTargetCommand {
            code: self.code,
            probe_pk,
            target_type,
            ip_address,
            target_pk,
        })?;

        writeln!(out, "Signature: {sig}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geoclicommand::MockGeoCliCommand;
    use doublezero_geolocation::state::{accounttype::AccountType, geo_probe::GeoProbe};
    use doublezero_sdk::geolocation::geo_probe::get::GetGeoProbeCommand;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

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
            reference_count: 1,
        }
    }

    #[test]
    fn test_cli_remove_target_outbound() {
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
            .expect_remove_target()
            .with(predicate::eq(RemoveTargetCommand {
                code: "geo-user-01".to_string(),
                probe_pk,
                target_type: GeoLocationTargetType::Outbound,
                ip_address: Ipv4Addr::new(8, 8, 8, 8),
                target_pk: Pubkey::default(),
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = RemoveTargetCliCommand {
            code: "geo-user-01".to_string(),
            target_type: TargetType::Outbound,
            target_ip: Some(Ipv4Addr::new(8, 8, 8, 8)),
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
    fn test_cli_remove_target_inbound() {
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
            .expect_remove_target()
            .with(predicate::eq(RemoveTargetCommand {
                code: "geo-user-01".to_string(),
                probe_pk,
                target_type: GeoLocationTargetType::Inbound,
                ip_address: Ipv4Addr::UNSPECIFIED,
                target_pk,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = RemoveTargetCliCommand {
            code: "geo-user-01".to_string(),
            target_type: TargetType::Inbound,
            target_ip: None,
            target_pk: Some(target_pk.to_string()),
            probe: Some(probe_pk.to_string()),
            exchange: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }
}
