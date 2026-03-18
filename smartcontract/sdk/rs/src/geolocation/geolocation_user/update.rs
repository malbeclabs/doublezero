use doublezero_geolocation::{
    instructions::{GeolocationInstruction, UpdateGeolocationUserArgs},
    pda,
    validation::validate_code_length,
};
use doublezero_program_common::validate_account_code;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateGeolocationUserCommand {
    pub code: String,
    pub token_account: Option<Pubkey>,
}

impl UpdateGeolocationUserCommand {
    pub fn execute(&self, client: &dyn GeolocationClient) -> eyre::Result<Signature> {
        if self.token_account.is_none() {
            return Err(eyre::eyre!("at least one field must be set"));
        }

        validate_code_length(&self.code)?;
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let program_id = client.get_program_id();
        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, &code);

        client.execute_transaction(
            GeolocationInstruction::UpdateGeolocationUser(UpdateGeolocationUserArgs {
                token_account: self.token_account,
            }),
            vec![AccountMeta::new(user_pda, false)],
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
    use mockall::predicate;

    #[test]
    fn test_update_geolocation_user_command() {
        let mut client = MockGeolocationClient::new();

        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let code = "geo-user-01";
        let new_token = Pubkey::new_unique();

        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, code);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(GeolocationInstruction::UpdateGeolocationUser(
                    UpdateGeolocationUserArgs {
                        token_account: Some(new_token),
                    },
                )),
                predicate::eq(vec![AccountMeta::new(user_pda, false)]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = UpdateGeolocationUserCommand {
            code: code.to_string(),
            token_account: Some(new_token),
        };

        let result = command.execute(&client);
        assert!(result.is_ok());
    }

    #[test]
    fn test_update_geolocation_user_all_none_is_error() {
        let client = MockGeolocationClient::new();

        let command = UpdateGeolocationUserCommand {
            code: "geo-user-01".to_string(),
            token_account: None,
        };

        let result = command.execute(&client);
        assert!(result.is_err());
    }
}
