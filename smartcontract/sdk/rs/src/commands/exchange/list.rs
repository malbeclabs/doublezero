use std::collections::HashMap;

use crate::DoubleZeroClient;
use doublezero_serviceability::{
    error::DoubleZeroError,
    state::{accountdata::AccountData, accounttype::AccountType, exchange::Exchange},
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct ListExchangeCommand {}

impl ListExchangeCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<HashMap<Pubkey, Exchange>> {
        client
            .gets(AccountType::Exchange)?
            .into_iter()
            .map(|(k, v)| {
                if let AccountData::Exchange(exchange) = v {
                    Ok((k, exchange))
                } else {
                    Err(DoubleZeroError::InvalidAccountType.into())
                }
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{commands::exchange::list::ListExchangeCommand, tests::utils::create_test_client};
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        exchange::{Exchange, ExchangeStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_exchange_list_command() {
        let mut client = create_test_client();

        let exchange1_pubkey = Pubkey::new_unique();
        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 0,
            code: "exchange1_code".to_string(),
            name: "exchange1_name".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
            status: ExchangeStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        let exchange2_pubkey = Pubkey::new_unique();
        let exchange2 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 0,
            code: "exchange2_code".to_string(),
            name: "exchange2_name".to_string(),
            lat: 3.0,
            lng: 4.0,
            loc_id: 5,
            status: ExchangeStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Exchange))
            .returning(move |_| {
                let mut exchanges = HashMap::new();
                exchanges.insert(exchange1_pubkey, AccountData::Exchange(exchange1.clone()));
                exchanges.insert(exchange2_pubkey, AccountData::Exchange(exchange2.clone()));
                Ok(exchanges)
            });

        // Search by pubkey
        let res = ListExchangeCommand {}.execute(&client);

        assert!(res.is_ok());
        let list = res.unwrap();
        assert!(list.len() == 2);
        assert!(list.contains_key(&exchange1_pubkey));
    }
}
