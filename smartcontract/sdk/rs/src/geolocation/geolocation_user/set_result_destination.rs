use doublezero_geolocation::{
    instructions::{GeolocationInstruction, SetResultDestinationArgs},
    pda,
    validation::validate_code_length,
};
use doublezero_program_common::validate_account_code;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct SetResultDestinationCommand {
    pub code: String,
    pub destination: String,
    pub probe_pks: Vec<Pubkey>,
}

impl SetResultDestinationCommand {
    pub fn execute(&self, client: &dyn GeolocationClient) -> eyre::Result<Signature> {
        validate_code_length(&self.code)?;
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let program_id = client.get_program_id();
        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, &code);

        let mut accounts = vec![AccountMeta::new(user_pda, false)];
        for probe_pk in &self.probe_pks {
            accounts.push(AccountMeta::new(*probe_pk, false));
        }

        client.execute_transaction(
            GeolocationInstruction::SetResultDestination(SetResultDestinationArgs {
                destination: self.destination.clone(),
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
    use mockall::predicate;

    #[test]
    fn test_set_result_destination() {
        let mut client = MockGeolocationClient::new();

        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let code = "geo-user-01";
        let probe_pk1 = Pubkey::new_unique();
        let probe_pk2 = Pubkey::new_unique();

        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, code);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(GeolocationInstruction::SetResultDestination(
                    SetResultDestinationArgs {
                        destination: "185.199.108.1:9000".to_string(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(user_pda, false),
                    AccountMeta::new(probe_pk1, false),
                    AccountMeta::new(probe_pk2, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = SetResultDestinationCommand {
            code: code.to_string(),
            destination: "185.199.108.1:9000".to_string(),
            probe_pks: vec![probe_pk1, probe_pk2],
        };

        let result = command.execute(&client);
        assert!(result.is_ok());
    }

    #[test]
    fn test_set_result_destination_clear() {
        let mut client = MockGeolocationClient::new();

        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let code = "geo-user-01";

        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, code);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(GeolocationInstruction::SetResultDestination(
                    SetResultDestinationArgs {
                        destination: String::new(),
                    },
                )),
                predicate::eq(vec![AccountMeta::new(user_pda, false)]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = SetResultDestinationCommand {
            code: code.to_string(),
            destination: String::new(),
            probe_pks: vec![],
        };

        let result = command.execute(&client);
        assert!(result.is_ok());
    }
}
