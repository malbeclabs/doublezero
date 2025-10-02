use anyhow::Result;
use bytemuck::Pod;
use doublezero_program_tools::{zero_copy, PrecomputedDiscriminator};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::pubkey::Pubkey;

#[derive(Debug)]
pub struct ZeroCopyAccountOwned<T: Pod + PrecomputedDiscriminator> {
    pub data: Option<(Box<T>, Vec<u8>)>,
    pub lamports: u64,
    pub balance: u64,
    pub owner: Pubkey,
}

impl<T: Pod + PrecomputedDiscriminator> ZeroCopyAccountOwned<T> {
    pub async fn from_rpc_client(rpc_client: &RpcClient, account_key: &Pubkey) -> Result<Self> {
        let account_info = rpc_client.get_account(account_key).await?;

        let data = zero_copy::checked_from_bytes_with_discriminator(&account_info.data)
            .map(|(mucked_data, remaining_data)| (Box::new(*mucked_data), remaining_data.to_vec()));

        let lamports = account_info.lamports;

        let rent_exemption = rpc_client
            .get_minimum_balance_for_rent_exemption(zero_copy::data_end::<T>())
            .await?;
        let balance = lamports.saturating_sub(rent_exemption);

        Ok(Self {
            data,
            lamports,
            balance,
            owner: account_info.owner,
        })
    }
}
