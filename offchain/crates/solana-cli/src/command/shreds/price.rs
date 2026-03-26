use std::collections::HashMap;

use anyhow::Result;
use clap::Args;
use doublezero_serviceability::state::{device::Device, exchange::Exchange};
use doublezero_solana_client_tools::rpc::SolanaConnectionOptions;
use doublezero_solana_sdk::shred_subscription::{self, state};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::{account::Account, pubkey::Pubkey};
use tabled::{
    Table, Tabled,
    settings::{Remove, Style, location::ByColumnName},
};

use super::make_dz_connection;

/*
   doublezero-solana shreds price [--device <PUBKEY> | --device-code <CODE> | --metro <PUBKEY>]
*/

#[derive(Debug, Args)]
pub struct PriceCommand {
    /// Filter by device.
    #[command(flatten)]
    device_args: super::DeviceArgs,

    /// Filter by metro exchange public key.
    #[arg(long, group = "device_id")]
    metro: Option<Pubkey>,

    #[arg(long)]
    wide: bool,

    #[arg(long)]
    json: bool,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

#[derive(Debug, Tabled, serde::Serialize)]
struct PriceRow {
    #[tabled(rename = "Device Code")]
    device_code: String,
    #[tabled(rename = "Device Pubkey")]
    device: String,
    #[tabled(rename = "Metro Code")]
    metro_code: String,
    #[tabled(rename = "Metro Name")]
    metro_name: String,
    #[tabled(rename = "Metro Pubkey")]
    metro: String,
    #[tabled(rename = "Status")]
    status: String,
    #[tabled(rename = "Settled Seats")]
    settled_seats: u16,
    #[tabled(rename = "Available Seats")]
    available_seats: u16,
    #[tabled(rename = "Base Price (USDC)")]
    base_price: i32,
    #[tabled(rename = "Premium (USDC)")]
    premium: i32,
    #[tabled(rename = "Epoch Price (USDC)")]
    epoch_price: i32,
}

impl PriceCommand {
    pub async fn try_into_execute(self, dz_ledger_url: Option<String>) -> Result<()> {
        let moniker_env = self.connection_options.moniker_env();
        let connection = self.connection_options.into_shred_subscription_connection();
        let network_env = match moniker_env {
            Some(env) => env,
            None => connection.try_network_environment().await?,
        };

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
            .get_program_accounts_with_config(&shred_subscription::ID, metro_config)
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

        if self.device_args.device.is_some() || self.device_args.device_code.is_some() {
            let device = self
                .device_args
                .resolve(network_env, &dz_ledger_url)
                .await?;
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
            .get_program_accounts_with_config(&shred_subscription::ID, device_config)
            .await?;

        if device_accounts.is_empty() {
            if self.json {
                println!("[]");
            } else {
                println!("No devices found.");
            }
            return Ok(());
        }

        // Parse DeviceHistory accounts and collect device keys
        let device_infos: Vec<state::DeviceHistoryInfo> = device_accounts
            .iter()
            .filter_map(|(_key, account)| state::parse_device_history(&account.data))
            .collect();

        // Fetch Device accounts from DZ Ledger for code, status, etc.
        let device_keys: Vec<Pubkey> = device_infos.iter().map(|d| d.device_key).collect();
        let dz_connection = make_dz_connection(&dz_ledger_url, network_env);
        let dz_device_accounts = dz_connection.get_multiple_accounts(&device_keys).await?;

        let device_map: HashMap<Pubkey, Device> = device_keys
            .iter()
            .zip(dz_device_accounts.iter())
            .filter_map(|(key, account)| {
                let account = account.as_ref()?;
                let device = Device::try_from(account.data.as_slice()).ok()?;
                Some((*key, device))
            })
            .collect();

        // Fetch Exchange accounts from DZ Ledger for metro code/name
        let exchange_keys: Vec<Pubkey> = metro_map.keys().copied().collect();
        let dz_exchange_accounts = dz_connection.get_multiple_accounts(&exchange_keys).await?;

        let exchange_map: HashMap<Pubkey, Exchange> = exchange_keys
            .iter()
            .zip(dz_exchange_accounts.iter())
            .filter_map(|(key, account)| {
                let account = account.as_ref()?;
                let exchange = Exchange::try_from(account.data.as_slice()).ok()?;
                Some((*key, exchange))
            })
            .collect();

        // Join: compute epoch price per device
        let mut rows: Vec<PriceRow> = device_infos
            .iter()
            .filter_map(|device_info| {
                let metro_info = metro_map.get(&device_info.exchange_key)?;
                let base = metro_info.current_usdc_price as i32;
                let premium = device_info.current_premium as i32;
                let epoch_price = base + premium;

                let dz_device = device_map.get(&device_info.device_key);
                let device_code = dz_device
                    .map(|d| d.code.clone())
                    .unwrap_or_else(|| "?".to_string());
                let status = dz_device
                    .map(|d| d.status.to_string())
                    .unwrap_or_else(|| "?".to_string());

                let dz_exchange = exchange_map.get(&device_info.exchange_key);
                let metro_code = dz_exchange
                    .map(|e| e.code.clone())
                    .unwrap_or_else(|| "?".to_string());
                let metro_name = dz_exchange
                    .map(|e| e.name.clone())
                    .unwrap_or_else(|| "?".to_string());

                Some(PriceRow {
                    device_code,
                    device: device_info.device_key.to_string(),
                    metro_code,
                    metro_name,
                    metro: device_info.exchange_key.to_string(),
                    status,
                    settled_seats: device_info.granted_seat_count,
                    available_seats: device_info.total_available_seats,
                    base_price: base,
                    premium,
                    epoch_price,
                })
            })
            .collect();

        rows.sort_by(|a, b| {
            a.metro_code
                .cmp(&b.metro_code)
                .then(a.device_code.cmp(&b.device_code))
        });

        if self.json {
            println!("{}", serde_json::to_string_pretty(&rows)?);
        } else {
            println!("{} device(s) found:\n", rows.len());

            let mut table = Table::new(rows);
            if !self.wide {
                table
                    .with(Remove::column(ByColumnName::new("Device Pubkey")))
                    .with(Remove::column(ByColumnName::new("Metro Pubkey")));
            }
            table.with(Style::markdown());
            println!("{table}");
        }

        Ok(())
    }
}
