use crate::{
    geoclicommand::GeoCliCommand,
    validators::{validate_pubkey, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_sdk::geolocation::geo_probe::{
    get::GetGeoProbeCommand, update::UpdateGeoProbeCommand,
};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};

#[derive(Args, Debug)]
pub struct UpdateGeoProbeCliCommand {
    /// Probe pubkey or code to update
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub probe: String,
    /// Updated public IPv4 address
    #[arg(long)]
    pub public_ip: Option<Ipv4Addr>,
    /// Updated UDP listen port for location offsets
    #[arg(long)]
    pub port: Option<u16>,
    /// Updated signing public key
    #[arg(long, value_parser = validate_pubkey)]
    pub signing_pubkey: Option<String>,
}

impl UpdateGeoProbeCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        if self.public_ip.is_none() && self.port.is_none() && self.signing_pubkey.is_none() {
            return Err(eyre::eyre!(
                "At least one of --public-ip, --port, or --signing-pubkey is required"
            ));
        }

        let (_, resolved_probe) = client.get_geo_probe(GetGeoProbeCommand {
            pubkey_or_code: self.probe,
        })?;
        let code = resolved_probe.code;

        let metrics_publisher_pk = self
            .signing_pubkey
            .map(|mp| {
                mp.parse::<Pubkey>()
                    .map_err(|_| eyre::eyre!("invalid signing pubkey: {mp}"))
            })
            .transpose()?;

        let serviceability_globalstate_pk = client.get_serviceability_globalstate_pk();

        let sig = client.update_geo_probe(UpdateGeoProbeCommand {
            code,
            serviceability_globalstate_pk,
            public_ip: self.public_ip,
            location_offset_port: self.port,
            metrics_publisher_pk,
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
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_geo_probe_update() {
        let mut client = MockGeoCliCommand::new();

        let svc_gs_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client
            .expect_get_geo_probe()
            .with(predicate::eq(GetGeoProbeCommand {
                pubkey_or_code: "ams-probe-01".to_string(),
            }))
            .returning(move |_| {
                Ok((
                    Pubkey::new_unique(),
                    GeoProbe {
                        account_type: AccountType::GeoProbe,
                        owner: Pubkey::new_unique(),
                        metro_pk: Pubkey::new_unique(),
                        public_ip: Ipv4Addr::new(10, 0, 0, 1),
                        location_offset_port: 8923,
                        code: "ams-probe-01".to_string(),
                        parent_devices: vec![],
                        metrics_publisher_pk: Pubkey::new_unique(),
                        reference_count: 0,
                        target_update_count: 0,
                    },
                ))
            });

        client
            .expect_get_serviceability_globalstate_pk()
            .returning(move || svc_gs_pk);

        client
            .expect_update_geo_probe()
            .with(predicate::eq(UpdateGeoProbeCommand {
                code: "ams-probe-01".to_string(),
                serviceability_globalstate_pk: svc_gs_pk,
                public_ip: Some(Ipv4Addr::new(192, 168, 1, 1)),
                location_offset_port: None,
                metrics_publisher_pk: None,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = UpdateGeoProbeCliCommand {
            probe: "ams-probe-01".to_string(),
            public_ip: Some(Ipv4Addr::new(192, 168, 1, 1)),
            port: None,
            signing_pubkey: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }
}
