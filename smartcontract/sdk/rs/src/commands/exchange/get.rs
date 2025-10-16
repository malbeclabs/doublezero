use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, exchange::Exchange,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetExchangeCommand {
    pub pubkey_or_code: String,
}

impl GetExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Exchange)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Exchange(exchange) => Ok((pk, exchange)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Exchange)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Exchange(exchange) => exchange.code == self.pubkey_or_code,
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Exchange(exchange) => Ok((pk, exchange)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Exchange with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{commands::exchange::get::GetExchangeCommand, tests::utils::create_test_client};
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        exchange::{Exchange, ExchangeStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_exchange_get_command() {
        let mut client = create_test_client();

        let exchange_pubkey = Pubkey::new_unique();
        let exchange = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 0,
            reference_count: 0,
            code: "exchange_code".to_string(),
            name: "exchange_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 1.0,
            lng: 2.0,
            bgp_community: 3,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        let exchange2 = exchange.clone();
        client
            .expect_get()
            .with(predicate::eq(exchange_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange2.clone())));

        let exchange2 = exchange.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Exchange))
            .returning(move |_| {
                let mut exchanges = HashMap::new();
                exchanges.insert(exchange_pubkey, AccountData::Exchange(exchange2.clone()));
                Ok(exchanges)
            });

        // Search by pubkey
        let res = GetExchangeCommand {
            pubkey_or_code: exchange_pubkey.to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "exchange_code".to_string());

        // Search by code
        let res = GetExchangeCommand {
            pubkey_or_code: "exchange_code".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "exchange_code".to_string());

        // Invalid search
        let res = GetExchangeCommand {
            pubkey_or_code: "ssssssssssss".to_string(),
        }
        .execute(&client);
        assert!(res.is_err());

        // Search by invalid code
        let res = GetExchangeCommand {
            pubkey_or_code: "s_(%".to_string(),
        }
        .execute(&client);
        assert!(res.is_err());
    }
}
