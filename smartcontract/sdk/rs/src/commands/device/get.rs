use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_sla_program::state::{
    accountdata::AccountData, accounttype::AccountType, device::Device,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetDeviceCommand {
    pub pubkey_or_code: String,
}

impl GetDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Device)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Device(device) => Ok((pk, device)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Device)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Device(device) => device.code == self.pubkey_or_code,
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Device(device) => Ok((pk, device)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Device with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}
