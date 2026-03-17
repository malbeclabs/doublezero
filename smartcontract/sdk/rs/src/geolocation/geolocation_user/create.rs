use doublezero_geolocation::{
    instructions::{CreateGeolocationUserArgs, GeolocationInstruction},
    pda,
    validation::validate_code_length,
};
use doublezero_program_common::validate_account_code;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct CreateGeolocationUserCommand {
    pub code: String,
    pub token_account: Pubkey,
}

impl CreateGeolocationUserCommand {
    pub fn execute(&self, client: &dyn GeolocationClient) -> eyre::Result<(Signature, Pubkey)> {
        validate_code_length(&self.code)?;
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let program_id = client.get_program_id();
        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, &code);

        client
            .execute_transaction(
                GeolocationInstruction::CreateGeolocationUser(CreateGeolocationUserArgs {
                    code,
                    token_account: self.token_account,
                }),
                vec![AccountMeta::new(user_pda, false)],
            )
            .map(|sig| (sig, user_pda))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
    use mockall::predicate;

    #[test]
    fn test_create_geolocation_user_command() {
        let mut client = MockGeolocationClient::new();

        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let token_account = Pubkey::new_unique();
        let code = "geo-user-01";

        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, code);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(GeolocationInstruction::CreateGeolocationUser(
                    CreateGeolocationUserArgs {
                        code: code.to_string(),
                        token_account,
                    },
                )),
                predicate::eq(vec![AccountMeta::new(user_pda, false)]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = CreateGeolocationUserCommand {
            code: code.to_string(),
            token_account,
        };

        let invalid_command = CreateGeolocationUserCommand {
            code: "geo/user".to_string(),
            ..command.clone()
        };

        let res = invalid_command.execute(&client);
        assert!(res.is_err());

        let result = command.execute(&client);
        assert!(result.is_ok());
        let (_, returned_pda) = result.unwrap();
        assert_eq!(returned_pda, user_pda);
    }
}
