use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, feed::Feed,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetFeedCommand {
    pub pubkey_or_code: String,
}

impl GetFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Feed)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Feed(feed) => Ok((pk, feed)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Feed)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Feed(feed) => feed.code.eq_ignore_ascii_case(&self.pubkey_or_code),
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Feed(feed) => Ok((pk, feed)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Feed with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}

#[cfg(test)]
mod tests {
    use crate::{commands::feed::get::GetFeedCommand, tests::utils::create_test_client};
    use doublezero_serviceability::state::{
        accountdata::AccountData, accounttype::AccountType, feed::Feed,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_commands_feed_get_command() {
        let mut client = create_test_client();

        let feed_pubkey = Pubkey::new_unique();
        let feed = Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: "feed_code".to_string(),
            name: "feed_name".to_string(),
            reference_count: 0,
            metros: vec![],
        };

        let feed2 = feed.clone();
        client
            .expect_get()
            .with(predicate::eq(feed_pubkey))
            .returning(move |_| Ok(AccountData::Feed(feed2.clone())));

        let feed2 = feed.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Feed))
            .returning(move |_| {
                let mut feeds = HashMap::new();
                feeds.insert(feed_pubkey, AccountData::Feed(feed2.clone()));
                Ok(feeds)
            });

        // Search by pubkey
        let res = GetFeedCommand {
            pubkey_or_code: feed_pubkey.to_string(),
        }
        .execute(&client);
        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "feed_code".to_string());

        // Search by code
        let res = GetFeedCommand {
            pubkey_or_code: "feed_code".to_string(),
        }
        .execute(&client);
        assert!(res.is_ok());
        assert_eq!(res.unwrap().1.code, "feed_code".to_string());

        // Search by code UPPERCASE
        let res = GetFeedCommand {
            pubkey_or_code: "FEED_CODE".to_string(),
        }
        .execute(&client);
        assert!(res.is_ok());

        // Invalid search
        let res = GetFeedCommand {
            pubkey_or_code: "ssssssssssss".to_string(),
        }
        .execute(&client);
        assert!(res.is_err());
    }
}
