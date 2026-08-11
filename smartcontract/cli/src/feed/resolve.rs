use crate::doublezerocommand::CliCommand;
use doublezero_sdk::Feed;
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
pub(crate) fn get_feeds<C: CliCommand>(
    client: &C,
    keys: &[Pubkey],
) -> eyre::Result<HashMap<Pubkey, Feed>> {
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
