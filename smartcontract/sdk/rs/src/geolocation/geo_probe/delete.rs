use doublezero_geolocation::{
    instructions::GeolocationInstruction, 
    validation::validate_code_length,
    pda};
use doublezero_program_common::validate_account_code;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteGeoProbeCommand {
    pub code: String,
    pub serviceability_globalstate_pk: Pubkey,
}

impl DeleteGeoProbeCommand {
    pub fn execute(&self, client: &dyn GeolocationClient) -> eyre::Result<Signature> {
        validate_code_length(&self.code)?;
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let program_id = client.get_program_id();
        let (probe_pda, _) = pda::get_geo_probe_pda(&program_id, &code);
        let (config_pda, _) = pda::get_program_config_pda(&program_id);

        client.execute_transaction(
            GeolocationInstruction::DeleteGeoProbe,
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
    fn test_geolocation_geo_probe_delete_command() {
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
                predicate::eq(GeolocationInstruction::DeleteGeoProbe),
                predicate::eq(vec![
                    AccountMeta::new(probe_pda, false),
                    AccountMeta::new_readonly(config_pda, false),
                    AccountMeta::new_readonly(svc_gs, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = DeleteGeoProbeCommand {
            code: code.to_string(),
            serviceability_globalstate_pk: svc_gs,
        };

        let result = command.execute(&client);
        assert!(result.is_ok());
    }
}
