use doublezero_geolocation::{
    instructions::{GeolocationInstruction, UpdateGeoProbeArgs},
    validation::validate_code_length,
    pda,
};
use doublezero_program_common::validate_account_code;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateGeoProbeCommand {
    pub code: String,
    pub serviceability_globalstate_pk: Pubkey,
    pub public_ip: Option<Ipv4Addr>,
    pub location_offset_port: Option<u16>,
    pub metrics_publisher_pk: Option<Pubkey>,
}

impl UpdateGeoProbeCommand {
    pub fn execute(&self, client: &dyn GeolocationClient) -> eyre::Result<Signature> {
        if self.public_ip.is_none()
            && self.location_offset_port.is_none()
            && self.metrics_publisher_pk.is_none()
        {
            return Err(eyre::eyre!("at least one field must be set"));
        }

        validate_code_length(&self.code)?;
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let program_id = client.get_program_id();
        let (probe_pda, _) = pda::get_geo_probe_pda(&program_id, &code);
        let (config_pda, _) = pda::get_program_config_pda(&program_id);

        client.execute_transaction(
            GeolocationInstruction::UpdateGeoProbe(UpdateGeoProbeArgs {
                public_ip: self.public_ip,
                location_offset_port: self.location_offset_port,
                metrics_publisher_pk: self.metrics_publisher_pk,
            }),
            vec![
                AccountMeta::new(probe_pda, false),
                AccountMeta::new_readonly(config_pda, false),
                AccountMeta::new_readonly(self.serviceability_globalstate_pk, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
    use mockall::predicate;

    #[test]
    fn test_geolocation_geo_probe_update_command() {
        let mut client = MockGeolocationClient::new();

        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let svc_gs = Pubkey::new_unique();
        let code = "probe-ams";

        let (probe_pda, _) = pda::get_geo_probe_pda(&program_id, code);
        let (config_pda, _) = pda::get_program_config_pda(&program_id);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(GeolocationInstruction::UpdateGeoProbe(UpdateGeoProbeArgs {
                    public_ip: Some(Ipv4Addr::new(192, 168, 1, 1)),
                    location_offset_port: None,
                    metrics_publisher_pk: None,
                })),
                predicate::eq(vec![
                    AccountMeta::new(probe_pda, false),
                    AccountMeta::new_readonly(config_pda, false),
                    AccountMeta::new_readonly(svc_gs, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = UpdateGeoProbeCommand {
            code: code.to_string(),
            serviceability_globalstate_pk: svc_gs,
            public_ip: Some(Ipv4Addr::new(192, 168, 1, 1)),
            location_offset_port: None,
            metrics_publisher_pk: None,
        };

        let result = command.execute(&client);
        assert!(result.is_ok());
    }

    #[test]
    fn test_geolocation_geo_probe_update_command_all_none_is_error() {
        let client = MockGeolocationClient::new();

        let command = UpdateGeoProbeCommand {
            code: "probe-ams".to_string(),
            serviceability_globalstate_pk: Pubkey::new_unique(),
            public_ip: None,
            location_offset_port: None,
            metrics_publisher_pk: None,
        };

        let result = command.execute(&client);
        assert!(result.is_err());
    }
}
