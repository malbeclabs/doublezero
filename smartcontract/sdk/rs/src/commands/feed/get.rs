use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_serviceability::{
    pda::get_feed_pda,
    state::{accountdata::AccountData, accounttype::AccountType, feed::Feed},
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetFeedCommand {
    /// A feed pubkey, or a feed `code`. A code is only unambiguous together with an `exchange`
    /// (one `feed_key` is one `(code, exchange)`); resolving a bare code that matches more than one
    /// metro is rejected so `update`/`delete` can never hit the wrong metro.
    pub pubkey_or_code: String,
    /// The metro (exchange) that, with a `code`, identifies the exact feed. Ignored when
    /// `pubkey_or_code` is a pubkey.
    pub exchange: Option<Pubkey>,
}

impl GetFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Feed)> {
        // A pubkey resolves directly.
        if let Some(pk) = parse_pubkey(&self.pubkey_or_code) {
            return match client.get(pk)? {
                AccountData::Feed(feed) => Ok((pk, feed)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            };
        }

        // A code plus its exchange derives the exact PDA — unambiguous.
        if let Some(exchange) = self.exchange {
            let (pk, _) = get_feed_pda(&client.get_program_id(), &self.pubkey_or_code, &exchange);
            return match client.get(pk)? {
                AccountData::Feed(feed) => Ok((pk, feed)),
                _ => Err(eyre::eyre!(
                    "Feed with code {} in the given exchange not found",
                    self.pubkey_or_code
                )),
            };
        }

        // A bare code: resolve only if it matches exactly one feed. The same code can exist in
        // multiple metros, so refuse an ambiguous match rather than mutating an arbitrary one.
        let matches: Vec<(Pubkey, Feed)> = client
            .gets(AccountType::Feed)?
            .into_iter()
            .filter_map(|(pk, v)| match v {
                AccountData::Feed(feed) if feed.code.eq_ignore_ascii_case(&self.pubkey_or_code) => {
                    Some((pk, feed))
                }
                _ => None,
            })
            .collect();

        match matches.len() {
            0 => Err(eyre::eyre!(
                "Feed with code {} not found",
                self.pubkey_or_code
            )),
            1 => Ok(matches.into_iter().next().unwrap()),
            n => Err(eyre::eyre!(
                "Feed code {} is ambiguous: it exists in {} metros. Pass --exchange or the feed pubkey.",
                self.pubkey_or_code,
                n
            )),
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

    fn feed_with(code: &str, exchange: Pubkey) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: code.to_string(),
            name: code.to_string(),
            exchange,
            groups: vec![Pubkey::new_unique()],
        }
    }

    #[test]
    fn test_commands_feed_get_command() {
        let mut client = create_test_client();

        let feed_pubkey = Pubkey::new_unique();
        let feed = feed_with("feed_code", Pubkey::new_unique());

        let feed2 = feed.clone();
        client
            .expect_get()
            .with(predicate::eq(feed_pubkey))
            .returning(move |_| Ok(AccountData::Feed(feed2.clone())));

        let feed3 = feed.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Feed))
            .returning(move |_| {
                let mut feeds = HashMap::new();
                feeds.insert(feed_pubkey, AccountData::Feed(feed3.clone()));
                Ok(feeds)
            });

        // By pubkey.
        let res = GetFeedCommand {
            pubkey_or_code: feed_pubkey.to_string(),
            exchange: None,
        }
        .execute(&client);
        assert_eq!(res.unwrap().1.code, "feed_code");

        // By unique code.
        let res = GetFeedCommand {
            pubkey_or_code: "feed_code".to_string(),
            exchange: None,
        }
        .execute(&client);
        assert_eq!(res.unwrap().1.code, "feed_code");

        // Unknown code.
        let res = GetFeedCommand {
            pubkey_or_code: "nope".to_string(),
            exchange: None,
        }
        .execute(&client);
        assert!(res.is_err());
    }

    #[test]
    fn test_commands_feed_get_ambiguous_code_rejected() {
        let mut client = create_test_client();

        // Same code in two metros.
        let tokyo = (
            Pubkey::new_unique(),
            feed_with("hyperliquid", Pubkey::new_unique()),
        );
        let london = (
            Pubkey::new_unique(),
            feed_with("hyperliquid", Pubkey::new_unique()),
        );
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Feed))
            .returning(move |_| {
                let mut feeds = HashMap::new();
                feeds.insert(tokyo.0, AccountData::Feed(tokyo.1.clone()));
                feeds.insert(london.0, AccountData::Feed(london.1.clone()));
                Ok(feeds)
            });

        let err = GetFeedCommand {
            pubkey_or_code: "hyperliquid".to_string(),
            exchange: None,
        }
        .execute(&client)
        .unwrap_err();
        assert!(
            err.to_string().contains("ambiguous"),
            "expected ambiguity error, got: {err}"
        );
    }
}
