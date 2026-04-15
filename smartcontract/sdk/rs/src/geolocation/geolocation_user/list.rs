use std::collections::HashMap;

use doublezero_geolocation::state::{accounttype::AccountType, geolocation_user::GeolocationUser};
use solana_account_decoder::UiAccountEncoding;
use solana_rpc_client_api::{
    config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    filter::{Memcmp, MemcmpEncodedBytes, RpcFilterType},
};
use solana_sdk::pubkey::Pubkey;

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct ListGeolocationUserCommand;

impl ListGeolocationUserCommand {
    pub fn execute(
        &self,
        client: &dyn GeolocationClient,
    ) -> eyre::Result<HashMap<Pubkey, GeolocationUser>> {
        let program_id = client.get_program_id();
        let filters = vec![RpcFilterType::Memcmp(Memcmp::new(
            0,
            MemcmpEncodedBytes::Bytes(vec![AccountType::GeolocationUser as u8]),
        ))];

        let accounts = client.get_program_accounts(
            &program_id,
            RpcProgramAccountsConfig {
                filters: Some(filters),
                account_config: RpcAccountInfoConfig {
                    encoding: Some(UiAccountEncoding::Base64),
                    ..Default::default()
                },
                ..Default::default()
            },
        )?;

        accounts
            .into_iter()
            .map(|(pubkey, account)| {
                let user = GeolocationUser::try_from(&account.data[..]).map_err(|_| {
                    eyre::eyre!("Failed to deserialize GeolocationUser account {pubkey}")
                })?;
                Ok((pubkey, user))
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
    use doublezero_geolocation::state::geolocation_user::{
        GeolocationBillingConfig, GeolocationPaymentStatus, GeolocationUserStatus,
    };
    use solana_sdk::account::Account;

    fn make_geolocation_user(code: &str) -> GeolocationUser {
        GeolocationUser {
            account_type: AccountType::GeolocationUser,
            owner: Pubkey::new_unique(),
            code: code.to_string(),
            token_account: Pubkey::new_unique(),
            payment_status: GeolocationPaymentStatus::Delinquent,
            billing: GeolocationBillingConfig::default(),
            status: GeolocationUserStatus::Activated,
            targets: vec![],
            result_destination: String::new(),
        }
    }

    #[test]
    fn test_list_geolocation_users() {
        let mut client = MockGeolocationClient::new();
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let user1 = make_geolocation_user("geo-user-01");
        let user2 = make_geolocation_user("geo-user-02");
        let pk1 = Pubkey::new_unique();
        let pk2 = Pubkey::new_unique();

        let accounts = vec![
            (
                pk1,
                Account {
                    data: borsh::to_vec(&user1).unwrap(),
                    owner: program_id,
                    ..Account::default()
                },
            ),
            (
                pk2,
                Account {
                    data: borsh::to_vec(&user2).unwrap(),
                    owner: program_id,
                    ..Account::default()
                },
            ),
        ];

        client
            .expect_get_program_accounts()
            .returning(move |_, _| Ok(accounts.clone()));

        let cmd = ListGeolocationUserCommand;
        let result = cmd.execute(&client);
        assert!(result.is_ok());
        let users = result.unwrap();
        assert_eq!(users.len(), 2);
        assert_eq!(users[&pk1].code, "geo-user-01");
        assert_eq!(users[&pk2].code, "geo-user-02");
    }

    #[test]
    fn test_list_geolocation_users_empty() {
        let mut client = MockGeolocationClient::new();
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        client
            .expect_get_program_accounts()
            .returning(|_, _| Ok(vec![]));

        let cmd = ListGeolocationUserCommand;
        let result = cmd.execute(&client);
        assert!(result.is_ok());
        assert!(result.unwrap().is_empty());
    }
}
