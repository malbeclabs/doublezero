use std::{collections::HashMap, io::Write};

use anyhow::Result;
use clap::Args;
use doublezero_cli_core::CliContext;
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

    /// Show all devices, including those with no remaining seats.
    #[arg(long)]
    all: bool,

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
    // Price at the execution controller's `last_settled_epoch`, which an
    // instant seat allocation is priced from. Diverges from `epoch_price` for
    // one epoch after a reprice. `None` when the device or metro ring has no
    // entry for that epoch, in which case an instant allocation would fail
    // onchain.
    #[tabled(rename = "Instant Price (USDC)")]
    #[tabled(display("display_instant_allocation_price"))]
    instant_allocation_price: Option<u16>,
}

fn display_instant_allocation_price(price: &Option<u16>) -> String {
    match price {
        Some(price) => price.to_string(),
        None => "-".to_string(),
    }
}

impl PriceCommand {
    pub async fn execute(
        self,
        dz_ledger_url: Option<String>,
        ctx: &CliContext,
        out: &mut impl Write,
    ) -> Result<()> {
        let connection = crate::command::solana_connection(ctx, &self.connection_options);
        let network_env =
            crate::command::resolve_network_env(&connection, self.connection_options.moniker_env())
                .await?;

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
                writeln!(out, "[]")?;
            } else {
                writeln!(out, "No devices found.")?;
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

        let (execution_controller_key, _) = state::find_execution_controller_address();

        // Fetch all histories + the execution controller (one call) + exchanges
        // (one call) in parallel.
        let mut all_history_keys = Vec::with_capacity(dh_keys.len() + mh_keys.len() + 1);
        all_history_keys.extend_from_slice(&dh_keys);
        all_history_keys.extend_from_slice(&mh_keys);
        all_history_keys.push(execution_controller_key);

        let (history_accounts, exchange_accounts) = tokio::try_join!(
            connection.try_fetch_multiple_accounts(&all_history_keys),
            dz_connection.try_fetch_multiple_accounts(&exchange_keys),
        )?;

        let (dh_data, rest) = history_accounts.split_at(dh_keys.len());
        let (mh_data, execution_controller_data) = rest.split_at(mh_keys.len());

        // The epoch an instant seat allocation is priced from. Missing or
        // unparseable leaves the Instant Price column empty rather than failing
        // the whole listing — every other column stands on its own.
        let last_settled_epoch = execution_controller_data.first().and_then(|account| {
            state::parse_execution_controller_last_settled_epoch(&account.data)
        });

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

        let settled_metro_prices: HashMap<Pubkey, u16> = exchange_keys
            .iter()
            .zip(mh_data.iter())
            .filter_map(|(exchange_key, account)| {
                let price_dollars =
                    state::parse_metro_history_price_at_epoch(&account.data, last_settled_epoch?)?;
                Some((*exchange_key, price_dollars))
            })
            .collect();

        let settled_device_premiums: HashMap<Pubkey, i16> = device_keys
            .iter()
            .zip(dh_data.iter())
            .filter_map(|(device_key, account)| {
                let premium_dollars = state::parse_device_history_premium_at_epoch(
                    &account.data,
                    last_settled_epoch?,
                )?;
                Some((*device_key, premium_dollars))
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
                writeln!(out, "[]")?;
            } else {
                writeln!(out, "No devices found.")?;
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
                    instant_allocation_price: settled_metro_prices
                        .get(&device_info.exchange_key)
                        .zip(settled_device_premiums.get(&device_info.device_key))
                        .map(|(metro_price_dollars, premium_dollars)| {
                            state::seat_usdc_price_dollars(*metro_price_dollars, *premium_dollars)
                        }),
                })
            })
            .collect();

        let total_count = rows.len();
        if !self.all {
            rows.retain(|row| row.settled_seats < row.available_seats);
        }
        let hidden_count = total_count - rows.len();

        if rows.is_empty() {
            if self.json {
                writeln!(out, "[]")?;
            } else if hidden_count > 0 {
                writeln!(
                    out,
                    "No devices with remaining seats found ({hidden_count} device(s) hidden, use --all to show)."
                )?;
            } else {
                writeln!(out, "No devices found.")?;
            }
            return Ok(());
        }

        rows.sort_by(|a, b| {
            a.metro_code
                .cmp(&b.metro_code)
                .then(a.device_code.cmp(&b.device_code))
        });

        if self.json {
            writeln!(out, "{}", serde_json::to_string_pretty(&rows)?)?;
        } else {
            if hidden_count > 0 {
                writeln!(
                    out,
                    "{} device(s) found ({} with no remaining seats hidden, use --all to show):\n",
                    rows.len(),
                    hidden_count,
                )?;
            } else {
                writeln!(out, "{} device(s) found:\n", rows.len())?;
            }

            match last_settled_epoch {
                Some(epoch) => writeln!(
                    out,
                    "Instant Price is the remainder-of-epoch price for epoch \
                     {epoch}. Epoch Price applies from the next settlement.\n"
                )?,
                None => writeln!(
                    out,
                    "Instant Price is unavailable: execution controller \
                     {execution_controller_key} is missing or unparseable.\n"
                )?,
            }

            let mut table = Table::new(rows);
            if !self.wide {
                table
                    .with(Remove::column(ByColumnName::new("Device Pubkey")))
                    .with(Remove::column(ByColumnName::new("Metro Pubkey")));
            }
            table.with(Style::markdown());
            writeln!(out, "{table}")?;
        }

        Ok(())
    }
}
