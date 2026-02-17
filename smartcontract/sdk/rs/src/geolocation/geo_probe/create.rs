use doublezero_geolocation::{
    instructions::{CreateGeoProbeArgs, GeolocationInstruction},
    validation::validate_code_length,
    pda,
};
use doublezero_program_common::validate_account_code;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct CreateGeoProbeCommand {
    pub exchange_pk: Pubkey,
    pub serviceability_globalstate_pk: Pubkey,
    pub code: String,
    pub public_ip: Ipv4Addr,
    pub location_offset_port: u16,
    pub metrics_publisher_pk: Pubkey,
}

impl CreateGeoProbeCommand {
    pub fn execute(&self, client: &dyn GeolocationClient) -> eyre::Result<(Signature, Pubkey)> {
        validate_code_length(&self.code)?;
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let program_id = client.get_program_id();
        let (probe_pda, _) = pda::get_geo_probe_pda(&program_id, &code);
        let (config_pda, _) = pda::get_program_config_pda(&program_id);

        client
            .execute_transaction(
                GeolocationInstruction::CreateGeoProbe(CreateGeoProbeArgs {
                    code,
                    public_ip: self.public_ip,
                    location_offset_port: self.location_offset_port,
                    metrics_publisher_pk: self.metrics_publisher_pk,
                }),
                vec![
                    AccountMeta::new(probe_pda, false),
                    AccountMeta::new_readonly(self.exchange_pk, false),
                    AccountMeta::new_readonly(config_pda, false),
                    AccountMeta::new_readonly(self.serviceability_globalstate_pk, false),
                ],
            )
            .map(|sig| (sig, probe_pda))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
    use mockall::predicate;

    #[test]
    fn test_geolocation_geo_probe_create_command() {
        let mut client = MockGeolocationClient::new();

        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let exchange = Pubkey::new_unique();
        let svc_gs = Pubkey::new_unique();
        let metrics_pk = Pubkey::new_unique();
        let code = "probe-ams";

        let (probe_pda, _) = pda::get_geo_probe_pda(&program_id, code);
        let (config_pda, _) = pda::get_program_config_pda(&program_id);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(GeolocationInstruction::CreateGeoProbe(CreateGeoProbeArgs {
                    code: code.to_string(),
                    public_ip: Ipv4Addr::new(10, 0, 0, 1),
                    location_offset_port: 8080,
                    metrics_publisher_pk: metrics_pk,
                })),
                predicate::eq(vec![
                    AccountMeta::new(probe_pda, false),
                    AccountMeta::new_readonly(exchange, false),
                    AccountMeta::new_readonly(config_pda, false),
                    AccountMeta::new_readonly(svc_gs, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = CreateGeoProbeCommand {
            exchange_pk: exchange,
            serviceability_globalstate_pk: svc_gs,
            code: code.to_string(),
            public_ip: Ipv4Addr::new(10, 0, 0, 1),
            location_offset_port: 8080,
            metrics_publisher_pk: metrics_pk,
        };

        let invalid_command = CreateGeoProbeCommand {
            code: "probe/ams".to_string(),
            ..command.clone()
        };

        let res = invalid_command.execute(&client);
        assert!(res.is_err());

        let result = command.execute(&client);
        assert!(result.is_ok());
        let (_, returned_pda) = result.unwrap();
        assert_eq!(returned_pda, probe_pda);
    }
}
