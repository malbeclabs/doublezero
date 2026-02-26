use crate::{
    geoclicommand::GeoCliCommand,
    validators::{validate_code, validate_pubkey},
};
use clap::Args;
use doublezero_sdk::geolocation::geo_probe::remove_parent_device::RemoveParentDeviceCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct RemoveParentGeoProbeCliCommand {
    /// Probe code
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Device account pubkey to remove as parent
    #[arg(long, value_parser = validate_pubkey)]
    pub device: String,
}

impl RemoveParentGeoProbeCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let device_pk = Pubkey::try_from(self.device.as_str()).unwrap();
        let serviceability_globalstate_pk = client.get_serviceability_globalstate_pk();

        let sig = client.remove_parent_device(RemoveParentDeviceCommand {
            code: self.code,
            device_pk,
            serviceability_globalstate_pk,
        })?;

        writeln!(out, "Signature: {sig}")?;

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
    fn test_cli_geo_probe_remove_parent() {
        let mut client = MockGeoCliCommand::new();

        let device_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let svc_gs_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
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
            .expect_remove_parent_device()
            .with(predicate::eq(RemoveParentDeviceCommand {
                code: "ams-probe-01".to_string(),
                device_pk,
                serviceability_globalstate_pk: svc_gs_pk,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = RemoveParentGeoProbeCliCommand {
            code: "ams-probe-01".to_string(),
            device: device_pk.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }
}
