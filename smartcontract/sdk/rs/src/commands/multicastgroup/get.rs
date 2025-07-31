use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_program_common::normalize_account_code;
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, multicastgroup::MulticastGroup,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetMulticastGroupCommand {
    pub pubkey_or_code: String,
}

impl GetMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, MulticastGroup)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::MulticastGroup(multicastgroup) => Ok((pk, multicastgroup)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => {
                let code = normalize_account_code(&self.pubkey_or_code)
                    .map_err(|err| eyre::eyre!("invalid code: {err}"))?;
                client
                    .gets(AccountType::MulticastGroup)?
                    .into_iter()
                    .find(|(_, v)| match v {
                        AccountData::MulticastGroup(multicastgroup) => multicastgroup.code == code,
                        _ => false,
                    })
                    .map(|(pk, v)| match v {
                        AccountData::MulticastGroup(multicastgroup) => Ok((pk, multicastgroup)),
                        _ => Err(eyre::eyre!("Invalid Account Type")),
                    })
                    .unwrap_or_else(|| {
                        Err(eyre::eyre!(
                            "MulticastGroup with code {} not found",
                            self.pubkey_or_code
                        ))
                    })
            }
        }
    }
}
