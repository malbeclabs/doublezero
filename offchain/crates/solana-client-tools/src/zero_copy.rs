use std::ops::Deref;

use anyhow::{Context, Result};
use bytemuck::Pod;
use doublezero_program_tools::{PrecomputedDiscriminator, zero_copy};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{pubkey::Pubkey, rent::Rent};

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ZeroCopyAccountOwnedData<T: Pod + PrecomputedDiscriminator> {
    pub mucked_data: Box<T>,
    pub remaining_data: Vec<u8>,
}

impl<T: Pod + PrecomputedDiscriminator> ZeroCopyAccountOwnedData<T> {
    pub fn new(data: &[u8]) -> Option<Self> {
        zero_copy::checked_from_bytes_with_discriminator(data).map(
            |(mucked_data, remaining_data)| ZeroCopyAccountOwnedData {
                mucked_data: Box::new(*mucked_data),
                remaining_data: remaining_data.to_vec(),
            },
        )
    }
}

impl<T: Pod + PrecomputedDiscriminator> Deref for ZeroCopyAccountOwnedData<T> {
    type Target = T;

    fn deref(&self) -> &Self::Target {
        &self.mucked_data
    }
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ZeroCopyAccountOwned<T: Pod + PrecomputedDiscriminator> {
    pub data: Option<ZeroCopyAccountOwnedData<T>>,
    pub data_len: usize,
    pub lamports: u64,
    pub balance: u64,
    pub owner: Pubkey,
}

impl<T: Pod + PrecomputedDiscriminator> ZeroCopyAccountOwned<T> {
    pub async fn try_from_rpc_client(rpc_client: &RpcClient, account_key: &Pubkey) -> Result<Self> {
        let account_info = rpc_client.get_account(account_key).await?;

        let data_len = account_info.data.len();
        let lamports = account_info.lamports;
        let rent_exemption_lamports = rpc_client
            .get_minimum_balance_for_rent_exemption(data_len)
            .await?;

        Ok(Self {
            data: ZeroCopyAccountOwnedData::new(&account_info.data),
            data_len,
            lamports,
            balance: lamports.saturating_sub(rent_exemption_lamports),
            owner: account_info.owner,
        })
    }

    pub fn try_data(&self) -> Result<&ZeroCopyAccountOwnedData<T>> {
        self.data
            .as_ref()
            .with_context(|| failed_read_zero_copy_as_type::<T>())
    }

    pub fn balance(&self, rent: &Rent) -> u64 {
        let rent_exemption_lamports = rent.minimum_balance(self.data_len);
        self.lamports.saturating_sub(rent_exemption_lamports)
    }
}

impl<T: Pod + PrecomputedDiscriminator> TryFrom<ZeroCopyAccountOwned<T>>
    for ZeroCopyAccountOwnedData<T>
{
    type Error = anyhow::Error;

    fn try_from(account: ZeroCopyAccountOwned<T>) -> Result<Self> {
        account
            .data
            .with_context(failed_read_zero_copy_as_type::<T>)
    }
}

fn failed_read_zero_copy_as_type<T: Pod + PrecomputedDiscriminator>() -> String {
    format!(
        "Cannot read zero-copy as type {}",
        std::any::type_name::<T>()
    )
}
