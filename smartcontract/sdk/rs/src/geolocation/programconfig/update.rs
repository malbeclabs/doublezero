use doublezero_geolocation::{
    instructions::{GeolocationInstruction, UpdateProgramConfigArgs},
    pda,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateProgramConfigCommand {
    pub version: Option<u32>,
    pub min_compatible_version: Option<u32>,
}

impl UpdateProgramConfigCommand {
    pub fn execute(&self, client: &dyn GeolocationClient) -> eyre::Result<Signature> {
        if self.version.is_none() && self.min_compatible_version.is_none() {
            return Err(eyre::eyre!("at least one field must be set"));
        }

        let program_id = client.get_program_id();
        let (config_pda, _) = pda::get_program_config_pda(&program_id);
        let program_data_pk = solana_loader_v3_interface::get_program_data_address(&program_id);

        client.execute_transaction(
            GeolocationInstruction::UpdateProgramConfig(UpdateProgramConfigArgs {
                version: self.version,
                min_compatible_version: self.min_compatible_version,
            }),
            vec![
                AccountMeta::new(config_pda, false),
                AccountMeta::new_readonly(program_data_pk, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_geolocation_programconfig_update_command() {
        let mut client = MockGeolocationClient::new();

        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let (config_pda, _) = pda::get_program_config_pda(&program_id);
        let program_data_pk = solana_loader_v3_interface::get_program_data_address(&program_id);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(GeolocationInstruction::UpdateProgramConfig(
                    UpdateProgramConfigArgs {
                        version: Some(2),
                        min_compatible_version: Some(1),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(config_pda, false),
                    AccountMeta::new_readonly(program_data_pk, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = UpdateProgramConfigCommand {
            version: Some(2),
            min_compatible_version: Some(1),
        };

        let result = command.execute(&client);
        assert!(result.is_ok());
    }

    #[test]
    fn test_geolocation_programconfig_update_command_all_none_is_error() {
        let client = MockGeolocationClient::new();

        let command = UpdateProgramConfigCommand {
            version: None,
            min_compatible_version: None,
        };

        let result = command.execute(&client);
        assert!(result.is_err());
    }
}
