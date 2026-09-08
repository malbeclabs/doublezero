use crate::{doublezerocommand::CliCommand, exchange::resolve::get_exchanges};
use doublezero_sdk::{Exchange, Feed};
use doublezero_serviceability::state::accountdata::AccountData;
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

pub(crate) fn pubkey_or_code(pubkey: Option<String>, code: Option<String>) -> eyre::Result<String> {
    match (pubkey, code) {
        (Some(pubkey), None) => Ok(pubkey),
        (None, Some(code)) => Ok(code),
        _ => eyre::bail!("pass --pubkey <PUBKEY>, or --code <CODE> with --exchange <EXCHANGE>"),
    }
}

/// Read the named feeds in one `getMultipleAccounts` call. A pass names at most a handful of feeds,
/// so this moves far fewer bytes than a scan of every feed account. A key that does not resolve to
/// a feed is left out of the map, and the caller falls back to printing the key.
fn get_feeds<C: CliCommand>(client: &C, keys: &[Pubkey]) -> eyre::Result<HashMap<Pubkey, Feed>> {
    if keys.is_empty() {
        return Ok(HashMap::new());
    }

    let accounts = client.get_multiple_accounts(keys.to_vec())?;
    let mut feeds = HashMap::new();
    for (key, account) in keys.iter().zip(accounts) {
        let Some(account) = account else { continue };
        if let Ok(AccountData::Feed(feed)) = AccountData::try_from(&account.data[..]) {
            feeds.insert(*key, feed);
        }
    }

    Ok(feeds)
}

/// The feeds a record names, together with the metros those feeds serve.
///
/// A feed is keyed by `(code, exchange)`, so the code alone does not name one: a pass holding the
/// same code in three metros holds three different feeds, and printing their codes renders the same
/// string three times. Every view that names a feed goes through [`FeedLabels::label`], so the
/// qualification is written once and the table and the JSON cannot disagree.
pub(crate) struct FeedLabels {
    feeds: HashMap<Pubkey, Feed>,
    exchanges: HashMap<Pubkey, Exchange>,
}

/// Read the named feeds, then the metros they serve. Two reads rather than one, because a feed's
/// exchange is only known once the feed itself is decoded; both are bounded by what one record
/// names, feeds sharing a metro read that metro once, and naming no feed reads nothing at all.
pub(crate) fn resolve_feed_labels<C: CliCommand>(
    client: &C,
    feed_keys: &[Pubkey],
) -> eyre::Result<FeedLabels> {
    let feeds = get_feeds(client, feed_keys)?;

    // Walk the caller's keys rather than the map: several feeds can serve one metro, and a
    // HashMap's iteration order is not stable, so this both asks for each metro once and keeps the
    // read deterministic.
    let mut exchange_keys: Vec<Pubkey> = Vec::new();
    for key in feed_keys {
        let Some(feed) = feeds.get(key) else { continue };
        if !exchange_keys.contains(&feed.exchange) {
            exchange_keys.push(feed.exchange);
        }
    }
    let exchanges = get_exchanges(client, &exchange_keys)?;

    Ok(FeedLabels { feeds, exchanges })
}

impl FeedLabels {
    /// The feed's code qualified by its metro, e.g. `qa-payments:xams`. A feed we cannot read is
    /// named by its key, which is unique and so needs no qualification.
    pub(crate) fn label(&self, feed_key: &Pubkey) -> String {
        match self.feeds.get(feed_key) {
            Some(feed) => format!("{}:{}", feed.code, self.metro_of(feed)),
            None => feed_key.to_string(),
        }
    }

    /// The feed's code on its own, as `--json` carries it.
    pub(crate) fn feed_code(&self, feed_key: &Pubkey) -> String {
        self.feeds
            .get(feed_key)
            .map_or(feed_key.to_string(), |feed| feed.code.clone())
    }

    /// The code of the metro this feed serves. Empty when the feed itself did not resolve: without
    /// the feed there is no exchange to name.
    pub(crate) fn exchange_code(&self, feed_key: &Pubkey) -> String {
        self.feeds
            .get(feed_key)
            .map_or(String::new(), |feed| self.metro_of(feed))
    }

    /// The feed account, for a caller that needs more than a name.
    pub(crate) fn feed(&self, feed_key: &Pubkey) -> Option<&Feed> {
        self.feeds.get(feed_key)
    }

    /// An exchange we cannot read is named by its key. That is uglier than dropping the suffix, but
    /// it keeps two same-coded feeds distinguishable, which is the point of qualifying them.
    fn metro_of(&self, feed: &Feed) -> String {
        self.exchanges
            .get(&feed.exchange)
            .map_or(feed.exchange.to_string(), |exchange| exchange.code.clone())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        feed::resolve::{resolve_feed_labels, FeedLabels},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{AccountType, Exchange, ExchangeStatus, Feed};
    use mockall::predicate;
    use solana_sdk::{account::Account, pubkey::Pubkey};
    use std::collections::HashMap;

    fn test_feed(code: &str, exchange: Pubkey) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: code.to_string(),
            name: code.to_string(),
            exchange,
            groups: vec![],
            ..Default::default()
        }
    }

    fn test_exchange(code: &str) -> Exchange {
        Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 255,
            lat: 52.37,
            lng: 4.89,
            bgp_community: 10001,
            unused: 0,
            status: ExchangeStatus::Activated,
            code: code.to_string(),
            name: code.to_string(),
            reference_count: 0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
        }
    }

    fn labels(feeds: Vec<(Pubkey, Feed)>, exchanges: Vec<(Pubkey, Exchange)>) -> FeedLabels {
        FeedLabels {
            feeds: feeds.into_iter().collect::<HashMap<_, _>>(),
            exchanges: exchanges.into_iter().collect::<HashMap<_, _>>(),
        }
    }

    #[test]
    fn test_feed_label_qualifies_the_code_with_its_metro() {
        let feed_key = Pubkey::new_unique();
        let exchange_key = Pubkey::new_unique();
        let labels = labels(
            vec![(feed_key, test_feed("qa-payments", exchange_key))],
            vec![(exchange_key, test_exchange("xams"))],
        );

        assert_eq!(labels.label(&feed_key), "qa-payments:xams");
        assert_eq!(labels.feed_code(&feed_key), "qa-payments");
        assert_eq!(labels.exchange_code(&feed_key), "xams");
    }

    /// Two feeds sharing a code are two different feeds; the metro is what tells them apart.
    #[test]
    fn test_feed_label_separates_one_code_held_in_two_metros() {
        let ams_feed = Pubkey::new_unique();
        let fra_feed = Pubkey::new_unique();
        let ams = Pubkey::new_unique();
        let fra = Pubkey::new_unique();
        let labels = labels(
            vec![
                (ams_feed, test_feed("lashay1-feed", ams)),
                (fra_feed, test_feed("lashay1-feed", fra)),
            ],
            vec![(ams, test_exchange("xams")), (fra, test_exchange("xfra"))],
        );

        assert_eq!(labels.label(&ams_feed), "lashay1-feed:xams");
        assert_eq!(labels.label(&fra_feed), "lashay1-feed:xfra");
    }

    #[test]
    fn test_feed_label_falls_back_to_the_exchange_key_when_the_metro_is_unresolved() {
        let feed_key = Pubkey::new_unique();
        let exchange_key = Pubkey::new_unique();
        let labels = labels(
            vec![(feed_key, test_feed("qa-payments", exchange_key))],
            vec![],
        );

        assert_eq!(
            labels.label(&feed_key),
            format!("qa-payments:{exchange_key}")
        );
        assert_eq!(labels.exchange_code(&feed_key), exchange_key.to_string());
    }

    #[test]
    fn test_feed_label_falls_back_to_the_feed_key_when_the_feed_is_unresolved() {
        let feed_key = Pubkey::new_unique();
        let labels = labels(vec![], vec![]);

        assert_eq!(labels.label(&feed_key), feed_key.to_string());
        assert_eq!(labels.feed_code(&feed_key), feed_key.to_string());
        assert_eq!(labels.exchange_code(&feed_key), "");
    }

    /// Feeds sharing a metro ask for that metro once, and the read follows the caller's key order
    /// rather than the feed map's, which is not stable across runs.
    #[test]
    fn test_resolve_feed_labels_reads_each_metro_once_in_key_order() {
        let mut client = create_test_client();
        let ams_feed = Pubkey::new_unique();
        let fra_feed = Pubkey::new_unique();
        let second_ams_feed = Pubkey::new_unique();
        let ams = Pubkey::new_unique();
        let fra = Pubkey::new_unique();

        let feeds = [
            test_feed("feed-a", ams),
            test_feed("feed-b", fra),
            test_feed("feed-c", ams),
        ];
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![ams_feed, fra_feed, second_ams_feed]))
            .returning(move |_| {
                Ok(feeds
                    .iter()
                    .map(|feed| {
                        Some(Account {
                            data: borsh::to_vec(feed).unwrap(),
                            ..Account::default()
                        })
                    })
                    .collect())
            });
        // Exactly `[ams, fra]`: `ams` deduped, and in the order the feed keys name it.
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![ams, fra]))
            .returning(|_| {
                Ok(vec![
                    Some(Account {
                        data: borsh::to_vec(&test_exchange("xams")).unwrap(),
                        ..Account::default()
                    }),
                    Some(Account {
                        data: borsh::to_vec(&test_exchange("xfra")).unwrap(),
                        ..Account::default()
                    }),
                ])
            });

        let labels = resolve_feed_labels(&client, &[ams_feed, fra_feed, second_ams_feed]).unwrap();

        assert_eq!(labels.label(&ams_feed), "feed-a:xams");
        assert_eq!(labels.label(&fra_feed), "feed-b:xfra");
        assert_eq!(labels.label(&second_ams_feed), "feed-c:xams");
    }

    /// A record naming no feed reads nothing, on either leg: no expectation is set here, so any RPC
    /// would panic.
    #[test]
    fn test_resolve_feed_labels_reads_nothing_without_feeds() {
        let client = create_test_client();

        let labels = resolve_feed_labels(&client, &[]).unwrap();
        assert!(labels.feeds.is_empty());
        assert!(labels.exchanges.is_empty());
    }
}
