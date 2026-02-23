use crate::{
    geoclicommand::GeoCliCommand,
    validators::{validate_code, validate_pubkey},
};
use clap::Args;
use doublezero_sdk::geolocation::geo_probe::create::CreateGeoProbeCommand;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr, str::FromStr};

#[derive(Args, Debug)]
pub struct CreateGeoProbeCliCommand {
    /// Unique probe code (e.g., "ams-probe-01")
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Exchange account pubkey
    #[arg(long, value_parser = validate_pubkey)]
    pub exchange: String,
    /// Public IPv4 address where probe listens
    #[arg(long)]
    pub public_ip: Ipv4Addr,
    /// UDP listen port for location offsets
    #[arg(long, default_value_t = 8923)]
    pub port: u16,
    /// Metrics publisher public key
    #[arg(long, value_parser = validate_pubkey)]
    pub metrics_publisher: String,
}

impl CreateGeoProbeCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let exchange_pk =
            Pubkey::from_str(&self.exchange).map_err(|_| eyre::eyre!("Invalid exchange pubkey"))?;

        let metrics_publisher_pk = if self.metrics_publisher == "me" {
            client.get_payer()
        } else {
            Pubkey::from_str(&self.metrics_publisher)
                .map_err(|_| eyre::eyre!("Invalid metrics publisher pubkey"))?
        };

        let serviceability_globalstate_pk = client.get_serviceability_globalstate_pk();

        let (sig, pda) = client.create_geo_probe(CreateGeoProbeCommand {
            exchange_pk,
            serviceability_globalstate_pk,
            code: self.code,
            public_ip: self.public_ip,
            location_offset_port: self.port,
            metrics_publisher_pk,
        })?;

        writeln!(out, "signature: {sig}\r\naccount: {pda}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geoclicommand::MockGeoCliCommand;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_geo_probe_create() {
        let mut client = MockGeoCliCommand::new();

        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let metrics_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let svc_gs_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let probe_pda = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client
            .expect_get_serviceability_globalstate_pk()
            .returning(move || svc_gs_pk);

        client
            .expect_create_geo_probe()
            .with(predicate::eq(CreateGeoProbeCommand {
                exchange_pk,
                serviceability_globalstate_pk: svc_gs_pk,
                code: "ams-probe-01".to_string(),
                public_ip: Ipv4Addr::new(10, 0, 0, 1),
                location_offset_port: 8923,
                metrics_publisher_pk: metrics_pk,
            }))
            .returning(move |_| Ok((signature, probe_pda)));

        let mut output = Vec::new();
        let res = CreateGeoProbeCliCommand {
            code: "ams-probe-01".to_string(),
            exchange: exchange_pk.to_string(),
            public_ip: Ipv4Addr::new(10, 0, 0, 1),
            port: 8923,
            metrics_publisher: metrics_pk.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("signature:"));
        assert!(output_str.contains(&probe_pda.to_string()));
    }
}
