use doublezero_geolocation::{
    pda, state::geolocation_user::GeolocationUser, validation::validate_code_length,
};
use doublezero_program_common::validate_account_code;
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct GetGeolocationUserCommand {
    pub pubkey_or_code: String,
}

impl GetGeolocationUserCommand {
    pub fn execute(
        &self,
        client: &dyn GeolocationClient,
    ) -> eyre::Result<(Pubkey, GeolocationUser)> {
        let program_id = client.get_program_id();

        let pubkey = match Pubkey::from_str(&self.pubkey_or_code) {
            Ok(pk) => pk,
            Err(_) => {
                validate_code_length(&self.pubkey_or_code)?;
                let code = validate_account_code(&self.pubkey_or_code)
                    .map_err(|err| eyre::eyre!("invalid code: {err}"))?;
                let (pda, _) = pda::get_geolocation_user_pda(&program_id, &code);
                pda
            }
        };

        let account = client.get_account(pubkey)?;
        let user = GeolocationUser::try_from(&account.data[..])
            .map_err(|_| eyre::eyre!("Failed to deserialize GeolocationUser account"))?;

        Ok((pubkey, user))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
    use doublezero_geolocation::state::{
        accounttype::AccountType,
        geolocation_user::{
            GeolocationBillingConfig, GeolocationPaymentStatus, GeolocationUserStatus,
        },
    };
    use solana_sdk::account::Account;

    fn make_geolocation_user(code: &str) -> GeolocationUser {
        GeolocationUser {
            account_type: AccountType::GeolocationUser,
            owner: Pubkey::new_unique(),
            update_count: 0,
            code: code.to_string(),
            token_account: Pubkey::new_unique(),
            payment_status: GeolocationPaymentStatus::Delinquent,
            billing: GeolocationBillingConfig::default(),
            status: GeolocationUserStatus::Activated,
            targets: vec![],
        }
    }

    #[test]
    fn test_get_geolocation_user_by_code() {
        let mut client = MockGeolocationClient::new();
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let code = "geo-user-01";
        let user = make_geolocation_user(code);
        let (expected_pda, _) = pda::get_geolocation_user_pda(&program_id, code);

        client
            .expect_get_account()
            .withf(move |pk| *pk == expected_pda)
            .returning(move |_| {
                Ok(Account {
                    data: borsh::to_vec(&user.clone()).unwrap(),
                    owner: program_id,
                    ..Account::default()
                })
            });

        let cmd = GetGeolocationUserCommand {
            pubkey_or_code: code.to_string(),
        };
        let result = cmd.execute(&client);
        assert!(result.is_ok());
        let (pk, returned_user) = result.unwrap();
        assert_eq!(pk, expected_pda);
        assert_eq!(returned_user.code, code);
    }

    #[test]
    fn test_get_geolocation_user_by_pubkey() {
        let mut client = MockGeolocationClient::new();
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let user_pk = Pubkey::new_unique();
        let user = make_geolocation_user("geo-user-02");

        client
            .expect_get_account()
            .withf(move |pk| *pk == user_pk)
            .returning(move |_| {
                Ok(Account {
                    data: borsh::to_vec(&user.clone()).unwrap(),
                    owner: program_id,
                    ..Account::default()
                })
            });

        let cmd = GetGeolocationUserCommand {
            pubkey_or_code: user_pk.to_string(),
        };
        let result = cmd.execute(&client);
        assert!(result.is_ok());
        let (pk, returned_user) = result.unwrap();
        assert_eq!(pk, user_pk);
        assert_eq!(returned_user.code, "geo-user-02");
    }
}
