use crate::{geoclicommand::GeoCliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_sdk::geolocation::geo_probe::get::GetGeoProbeCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetGeoProbeCliCommand {
    /// Probe pubkey or code to retrieve
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub code: String,
}

impl GetGeoProbeCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, probe) = client.get_geo_probe(GetGeoProbeCommand {
            pubkey_or_code: self.code,
        })?;

        writeln!(
            out,
            "account: {}\r\n\
code: {}\r\n\
owner: {}\r\n\
exchange: {}\r\n\
public_ip: {}\r\n\
port: {}\r\n\
parent_devices: {:?}\r\n\
metrics_publisher: {}\r\n\
reference_count: {}",
            pubkey,
            probe.code,
            probe.owner,
            probe.exchange_pk,
            probe.public_ip,
            probe.location_offset_port,
            probe.parent_devices,
            probe.metrics_publisher_pk,
            probe.reference_count,
        )?;

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

    #[test]
    fn test_cli_geo_probe_get() {
        let mut client = MockGeoCliCommand::new();

        let probe_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let owner_pk = Pubkey::from_str_const("DDddB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let metrics_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");

        let probe = GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: owner_pk,
            bump_seed: 255,
            exchange_pk,
            public_ip: Ipv4Addr::new(10, 0, 0, 1),
            location_offset_port: 8923,
            code: "ams-probe-01".to_string(),
            parent_devices: vec![],
            metrics_publisher_pk: metrics_pk,
            reference_count: 0,
        };

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
            code: Pubkey::new_unique().to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_err());

        let mut output = Vec::new();
        let res = GetGeoProbeCliCommand {
            code: probe_pk.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains(&format!("account: {probe_pk}")));
        assert!(output_str.contains("code: ams-probe-01"));
        assert!(output_str.contains(&format!("owner: {owner_pk}")));
        assert!(output_str.contains(&format!("exchange: {exchange_pk}")));
        assert!(output_str.contains("public_ip: 10.0.0.1"));
        assert!(output_str.contains("port: 8923"));
        assert!(output_str.contains("parent_devices: []"));
        assert!(output_str.contains(&format!("metrics_publisher: {metrics_pk}")));
        assert!(output_str.contains("reference_count: 0"));
    }
}
