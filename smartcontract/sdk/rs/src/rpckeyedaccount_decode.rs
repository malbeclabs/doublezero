use crate::AccountData;
use base64::{prelude::*, Engine};
use eyre::eyre;
use solana_account_decoder::{UiAccountData, UiAccountEncoding};
use solana_rpc_client_api::response::RpcKeyedAccount;
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;

pub fn rpckeyedaccount_decode(
    keyed_account: RpcKeyedAccount,
) -> eyre::Result<Option<(Box<Pubkey>, Box<AccountData>)>> {
    if let UiAccountData::Binary(data, UiAccountEncoding::Base64) = keyed_account.account.data {
        let pubkey = Box::new(
            Pubkey::from_str(&keyed_account.pubkey)
                .map_err(|e| eyre!("Unable to parse Pubkey:{e}"))?,
        );
        let bytes = BASE64_STANDARD
            .decode(data.clone())
            .map_err(|e| eyre!("Unable decode data: {e}"))?;
        let account = Box::new(AccountData::try_from(&bytes[..])?);
        return Ok(Some((pubkey, account)));
    }
    Ok(None)
}
