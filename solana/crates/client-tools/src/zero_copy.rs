use anyhow::{anyhow, Result};
use bytemuck::Pod;
use doublezero_program_tools::{zero_copy, PrecomputedDiscriminator};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::pubkey::Pubkey;

#[derive(Debug)]
pub struct ZeroCopyAccountOwned<T: Pod + PrecomputedDiscriminator> {
    pub data: T,
    pub remaining_data: Vec<u8>,
}

impl<T: Pod + PrecomputedDiscriminator> ZeroCopyAccountOwned<T> {
    pub async fn from_rpc_client(rpc_client: &RpcClient, account_key: &Pubkey) -> Result<Self> {
        let account_info = rpc_client.get_account(account_key).await?;

        let (mucked_data, remaining_data) =
            zero_copy::checked_from_bytes_with_discriminator(&account_info.data)
                .ok_or(anyhow!("cannot deserialize as plain-old-data"))?;

        Ok(Self {
            data: *mucked_data,
            remaining_data: remaining_data.to_vec(),
        })
    }
}
