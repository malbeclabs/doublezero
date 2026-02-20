use doublezero_geolocation::{
    instructions::{GeolocationInstruction, InitProgramConfigArgs},
    pda,
};
use solana_program::bpf_loader_upgradeable;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct InitProgramConfigCommand {
    pub serviceability_program_id: Pubkey,
}

impl InitProgramConfigCommand {
    pub fn execute(&self, client: &dyn GeolocationClient) -> eyre::Result<(Signature, Pubkey)> {
        let program_id = client.get_program_id();
        let (config_pda, _) = pda::get_program_config_pda(&program_id);
        let (program_data_pk, _) =
            Pubkey::find_program_address(&[program_id.as_ref()], &bpf_loader_upgradeable::id());

        client
            .execute_transaction(
                GeolocationInstruction::InitProgramConfig(InitProgramConfigArgs {
                    serviceability_program_id: self.serviceability_program_id,
                }),
                vec![
                    AccountMeta::new(config_pda, false),
                    AccountMeta::new_readonly(program_data_pk, false),
                ],
            )
            .map(|sig| (sig, config_pda))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
    use mockall::predicate;

    #[test]
    fn test_geolocation_programconfig_init_command() {
        let mut client = MockGeolocationClient::new();

        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let svc_id = Pubkey::new_unique();

        let (config_pda, _) = pda::get_program_config_pda(&program_id);
        let (program_data_pk, _) =
            Pubkey::find_program_address(&[program_id.as_ref()], &bpf_loader_upgradeable::id());

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(GeolocationInstruction::InitProgramConfig(
                    InitProgramConfigArgs {
                        serviceability_program_id: svc_id,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(config_pda, false),
                    AccountMeta::new_readonly(program_data_pk, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = InitProgramConfigCommand {
            serviceability_program_id: svc_id,
        };

        let result = command.execute(&client);
        assert!(result.is_ok());
        let (_, returned_pda) = result.unwrap();
        assert_eq!(returned_pda, config_pda);
    }
}
