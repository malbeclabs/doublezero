use std::collections::HashMap;

use anyhow::Result;
use clap::Args;
use doublezero_serviceability::state::{device::Device, exchange::Exchange};
use doublezero_solana_client_tools::rpc::SolanaConnectionOptions;
use doublezero_solana_sdk::shred_subscription::state;
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::pubkey::Pubkey;
use tabled::{
    Table, Tabled,
    settings::{Remove, Style, location::ByColumnName},
};

use super::{make_dz_connection, serviceability_program_id};

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

        let dz_connection = make_dz_connection(&dz_ledger_url, network_env);

        // Fetch Device accounts from DZ Ledger.
        let (device_keys, device_map): (Vec<Pubkey>, HashMap<Pubkey, Device>) =
            if self.device_args.device.is_some() || self.device_args.device_code.is_some() {
                let device_key = self
                    .device_args
                    .resolve(network_env, &dz_ledger_url)
                    .await?;
                let accounts = dz_connection.get_multiple_accounts(&[device_key]).await?;
                let mut map = HashMap::new();
                if let Some(Some(account)) = accounts.first()
                    && let Ok(device) = Device::try_from(account.data.as_slice())
                {
                    map.insert(device_key, device);
                }
                (vec![device_key], map)
            } else {
                let program_id = serviceability_program_id(network_env)?;
                let config = RpcProgramAccountsConfig {
                    filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                        0,
                        vec![5], // AccountType::Device
                    ))]),
                    account_config: RpcAccountInfoConfig {
                        encoding: Some(UiAccountEncoding::Base64),
                        ..Default::default()
                    },
                    ..Default::default()
                };
                let accounts = dz_connection
                    .get_program_accounts_with_config(&program_id, config)
                    .await?;
                let mut keys = Vec::new();
                let mut map = HashMap::new();
                for (key, account) in &accounts {
                    if let Ok(device) = Device::try_from(account.data.as_slice()) {
                        keys.push(*key);
                        map.insert(*key, device);
                    }
                }
                (keys, map)
            };

        // Apply --metro filter client-side.
        let (device_keys, device_map) = if let Some(metro) = self.metro {
            let filtered: HashMap<Pubkey, Device> = device_map
                .into_iter()
                .filter(|(_, d)| d.exchange_pk == metro)
                .collect();
            let keys: Vec<Pubkey> = device_keys
                .into_iter()
                .filter(|k| filtered.contains_key(k))
                .collect();
            (keys, filtered)
        } else {
            (device_keys, device_map)
        };

        if device_keys.is_empty() {
            if self.json {
                println!("[]");
            } else {
                println!("No devices found.");
            }
            return Ok(());
        }

        // Derive DeviceHistory + MetroHistory PDA addresses.
        let exchange_keys: Vec<Pubkey> = device_map
            .values()
            .map(|d| d.exchange_pk)
            .collect::<std::collections::HashSet<_>>()
            .into_iter()
            .collect();

        let dh_keys: Vec<Pubkey> = device_keys
            .iter()
            .map(|dk| state::find_device_history_address(dk).0)
            .collect();
        let mh_keys: Vec<Pubkey> = exchange_keys
            .iter()
            .map(|ek| state::find_metro_history_address(ek).0)
            .collect();

        // Fetch all histories (one call) + exchanges (one call) in parallel.
        let mut all_history_keys = Vec::with_capacity(dh_keys.len() + mh_keys.len());
        all_history_keys.extend_from_slice(&dh_keys);
        all_history_keys.extend_from_slice(&mh_keys);

        let exchange_fut = async {
            dz_connection
                .get_multiple_accounts(&exchange_keys)
                .await
                .map_err(Into::into)
        };
        let (history_accounts, exchange_accounts): (_, Vec<Option<_>>) = tokio::try_join!(
            connection.try_fetch_multiple_accounts(&all_history_keys),
            exchange_fut
        )?;

        let (dh_data, mh_data) = history_accounts.split_at(dh_keys.len());

        let device_infos: Vec<state::DeviceHistoryInfo> = dh_data
            .iter()
            .filter_map(|account| state::parse_device_history(&account.data))
            .collect();

        let metro_map: HashMap<Pubkey, state::MetroHistoryInfo> = mh_data
            .iter()
            .filter_map(|account| {
                let info = state::parse_metro_history(&account.data)?;
                Some((info.exchange_key, info))
            })
            .collect();

        let exchange_map: HashMap<Pubkey, Exchange> = exchange_keys
            .iter()
            .zip(exchange_accounts.iter())
            .filter_map(|(key, account)| {
                let account = account.as_ref()?;
                let exchange = Exchange::try_from(account.data.as_slice()).ok()?;
                Some((*key, exchange))
            })
            .collect();

        if device_infos.is_empty() {
            if self.json {
                println!("[]");
            } else {
                println!("No devices found.");
            }
            return Ok(());
        }

        // Join: compute epoch price per device.
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
