use std::collections::HashMap;

use anyhow::Result;
use clap::Args;
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};
use doublezero_solana_sdk::reservation::{self, state};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::{account::Account, pubkey::Pubkey};
use tabled::{Table, Tabled, settings::Style};

/*
   doublezero-solana reservation price [--device <PUBKEY>] [--metro <PUBKEY>]
*/

#[derive(Debug, Args)]
pub struct PriceCommand {
    /// Filter by device public key.
    #[arg(long, group = "filter")]
    device: Option<Pubkey>,

    /// Filter by metro exchange public key.
    #[arg(long, group = "filter")]
    metro: Option<Pubkey>,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

#[derive(Debug, Tabled)]
struct PriceRow {
    #[tabled(rename = "Device")]
    device: String,
    #[tabled(rename = "Metro")]
    metro: String,
    #[tabled(rename = "Base Price (USDC)")]
    base_price: String,
    #[tabled(rename = "Premium (USDC)")]
    premium: String,
    #[tabled(rename = "Total (USDC)")]
    total: String,
}

impl PriceCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let connection = SolanaConnection::from(self.connection_options);

        // Fetch all MetroHistory accounts
        let metro_disc_bytes = borsh::to_vec(&state::METRO_HISTORY_DISCRIMINATOR)
            .expect("discriminator serialization");
        let metro_filters = vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
            0,
            metro_disc_bytes,
        ))];

        let metro_config = RpcProgramAccountsConfig {
            filters: Some(metro_filters),
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                ..Default::default()
            },
            ..Default::default()
        };

        let metro_accounts: Vec<(Pubkey, Account)> = connection
            .get_program_accounts_with_config(&reservation::ID, metro_config)
            .await?;

        let metro_map: HashMap<Pubkey, state::MetroHistoryInfo> = metro_accounts
            .iter()
            .filter_map(|(_key, account)| {
                let info = state::parse_metro_history(&account.data)?;
                Some((info.exchange_key, info))
            })
            .collect();

        // Fetch DeviceHistory accounts
        let device_disc_bytes = borsh::to_vec(&state::DEVICE_HISTORY_DISCRIMINATOR)
            .expect("discriminator serialization");
        let mut device_filters = vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
            0,
            device_disc_bytes,
        ))];

        if let Some(device) = self.device {
            device_filters.push(RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                state::DEVICE_HISTORY_DEVICE_KEY_OFFSET,
                device.to_bytes().to_vec(),
            )));
        }

        if let Some(metro) = self.metro {
            device_filters.push(RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                state::DEVICE_HISTORY_EXCHANGE_KEY_OFFSET,
                metro.to_bytes().to_vec(),
            )));
        }

        let device_config = RpcProgramAccountsConfig {
            filters: Some(device_filters),
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                ..Default::default()
            },
            ..Default::default()
        };

        let device_accounts: Vec<(Pubkey, Account)> = connection
            .get_program_accounts_with_config(&reservation::ID, device_config)
            .await?;

        if device_accounts.is_empty() {
            println!("No devices found.");
            return Ok(());
        }

        // Join: compute total price per device
        let mut rows: Vec<PriceRow> = device_accounts
            .iter()
            .filter_map(|(_key, account)| {
                let device_info = state::parse_device_history(&account.data)?;
                let metro_info = metro_map.get(&device_info.exchange_key)?;
                let base = metro_info.current_usdc_price as i32;
                let premium = device_info.current_premium as i32;
                let total = base + premium;
                Some(PriceRow {
                    device: device_info.device_key.to_string(),
                    metro: device_info.exchange_key.to_string(),
                    base_price: base.to_string(),
                    premium: premium.to_string(),
                    total: total.to_string(),
                })
            })
            .collect();

        if rows.is_empty() {
            println!("No device pricing data available.");
            return Ok(());
        }

        rows.sort_by(|a, b| a.metro.cmp(&b.metro).then(a.device.cmp(&b.device)));

        println!("{} device(s) found:\n", rows.len());

        let mut table = Table::new(rows);
        table.with(Style::markdown());
        println!("{table}");

        Ok(())
    }
}
