use crate::doublezerocommand::CliCommand;
use doublezero_sdk::Exchange;
use doublezero_serviceability::state::accountdata::AccountData;
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

/// Read the named exchanges in one `getMultipleAccounts` call, the way [`crate::feed::resolve`]
/// reads feeds. A view names the handful of metros its records point at, so this moves far fewer
/// bytes than a scan of every exchange account. A key that does not resolve to an exchange is left
/// out of the map, and the caller falls back to printing the key.
pub(crate) fn get_exchanges<C: CliCommand>(
    client: &C,
    keys: &[Pubkey],
) -> eyre::Result<HashMap<Pubkey, Exchange>> {
    if keys.is_empty() {
        return Ok(HashMap::new());
    }

    let accounts = client.get_multiple_accounts(keys.to_vec())?;
    let mut exchanges = HashMap::new();
    for (key, account) in keys.iter().zip(accounts) {
        let Some(account) = account else { continue };
        if let Ok(AccountData::Exchange(exchange)) = AccountData::try_from(&account.data[..]) {
            exchanges.insert(*key, exchange);
        }
    }

    Ok(exchanges)
}

#[cfg(test)]
mod tests {
    use crate::{exchange::resolve::get_exchanges, tests::utils::create_test_client};
    use doublezero_sdk::{AccountType, Exchange, ExchangeStatus, Feed};
    use mockall::predicate;
    use solana_sdk::{account::Account, pubkey::Pubkey};

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

    /// No keys means no RPC. Every caller reaches this helper through a record that may name no
    /// exchange at all, and those callers set no `get_multiple_accounts` expectation.
    #[test]
    fn test_get_exchanges_reads_nothing_for_an_empty_key_list() {
        let client = create_test_client();

        let exchanges = get_exchanges(&client, &[]).unwrap();
        assert!(exchanges.is_empty());
    }

    #[test]
    fn test_get_exchanges_reads_the_named_exchanges() {
        let mut client = create_test_client();
        let ams = Pubkey::new_unique();
        let fra = Pubkey::new_unique();

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

        let exchanges = get_exchanges(&client, &[ams, fra]).unwrap();
        assert_eq!(exchanges[&ams].code, "xams");
        assert_eq!(exchanges[&fra].code, "xfra");
    }

    /// A key that holds no account, or holds something that is not an exchange, is left out rather
    /// than aborting the read: the caller prints the key and the rest of the record still renders.
    #[test]
    fn test_get_exchanges_skips_what_is_not_an_exchange() {
        let mut client = create_test_client();
        let missing = Pubkey::new_unique();
        let not_an_exchange = Pubkey::new_unique();
        let ams = Pubkey::new_unique();

        let feed = Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: "qa-payments".to_string(),
            name: "QA Payments".to_string(),
            exchange: Pubkey::new_unique(),
            groups: vec![],
            ..Default::default()
        };

        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![missing, not_an_exchange, ams]))
            .returning(move |_| {
                Ok(vec![
                    None,
                    Some(Account {
                        data: borsh::to_vec(&feed).unwrap(),
                        ..Account::default()
                    }),
                    Some(Account {
                        data: borsh::to_vec(&test_exchange("xams")).unwrap(),
                        ..Account::default()
                    }),
                ])
            });

        let exchanges = get_exchanges(&client, &[missing, not_an_exchange, ams]).unwrap();
        assert_eq!(exchanges.len(), 1);
        assert_eq!(exchanges[&ams].code, "xams");
    }
}
