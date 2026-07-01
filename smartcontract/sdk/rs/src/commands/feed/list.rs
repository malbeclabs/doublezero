use std::collections::HashMap;

use crate::DoubleZeroClient;
use doublezero_serviceability::{
    error::DoubleZeroError,
    state::{accountdata::AccountData, accounttype::AccountType, feed::Feed},
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct ListFeedCommand;

impl ListFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<HashMap<Pubkey, Feed>> {
        client
            .gets(AccountType::Feed)?
            .into_iter()
            .map(|(k, v)| {
                if let AccountData::Feed(feed) = v {
                    Ok((k, feed))
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

    use crate::{commands::feed::list::ListFeedCommand, tests::utils::create_test_client};
    use doublezero_serviceability::state::{
        accountdata::AccountData, accounttype::AccountType, feed::Feed,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_feed_list_command() {
        let mut client = create_test_client();

        let feed1_pubkey = Pubkey::new_unique();
        let feed1 = Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: "feed1_code".to_string(),
            name: "feed1_name".to_string(),
            reference_count: 0,
            metros: vec![],
        };

        let feed2_pubkey = Pubkey::new_unique();
        let feed2 = Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: "feed2_code".to_string(),
            name: "feed2_name".to_string(),
            reference_count: 0,
            metros: vec![],
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Feed))
            .returning(move |_| {
                let mut feeds = HashMap::new();
                feeds.insert(feed1_pubkey, AccountData::Feed(feed1.clone()));
                feeds.insert(feed2_pubkey, AccountData::Feed(feed2.clone()));
                Ok(feeds)
            });

        let res = ListFeedCommand.execute(&client);
        assert!(res.is_ok());
        let list = res.unwrap();
        assert!(list.len() == 2);
        assert!(list.contains_key(&feed1_pubkey));
    }
}
