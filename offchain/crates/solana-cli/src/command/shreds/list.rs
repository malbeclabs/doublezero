use std::{collections::HashMap, net::Ipv4Addr};

use anyhow::Result;
use clap::Args;
use doublezero_serviceability::state::device::Device;
use doublezero_solana_client_tools::rpc::{
    DoubleZeroLedgerConnection, SolanaConnection, SolanaConnectionOptions,
};
use doublezero_solana_sdk::reservation::{self, state};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::{account::Account, pubkey::Pubkey};
use tabled::{Table, Tabled, settings::Style};

/*
   doublezero-solana reservation list [--device <PUBKEY> | --device-code <CODE>]
*/

#[derive(Debug, Args)]
pub struct ListCommand {
    /// Filter seats by device.
    #[command(flatten)]
    device_args: super::DeviceArgs,

    /// Filter seats by withdraw authority (wallets that have payment escrows).
    #[arg(long)]
    withdraw_authority: Option<Pubkey>,

    /// Filter seats by client IPv4 address.
    #[arg(long)]
    client_ip: Option<Ipv4Addr>,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

#[derive(Debug, Tabled)]
struct SeatRow {
    #[tabled(rename = "Device Code")]
    device_code: String,
    #[tabled(rename = "Client IP")]
    client_ip: Ipv4Addr,
    #[tabled(rename = "Tenure")]
    tenure: u16,
    #[tabled(rename = "Balance (USDC)")]
    escrow_usdc: String,
    #[tabled(rename = "Est. Epochs Paid")]
    est_epochs_paid: String,
}

impl ListCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let connection = SolanaConnection::from(self.connection_options);

        let discriminator_bytes =
            borsh::to_vec(&state::CLIENT_SEAT_DISCRIMINATOR).expect("discriminator serialization");

        let mut filters = vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
            0,
            discriminator_bytes,
        ))];

        // Resolve device filter.
        let network_env = connection.try_network_environment().await?;
        if self.device_args.device.is_some() || self.device_args.device_code.is_some() {
            let device = self.device_args.resolve(network_env).await?;
            filters.push(RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                state::CLIENT_SEAT_DEVICE_KEY_OFFSET,
                device.to_bytes().to_vec(),
            )));
        }

        if let Some(client_ip) = self.client_ip {
            let ip_bits = u32::from(client_ip);
            filters.push(RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                state::CLIENT_SEAT_CLIENT_IP_OFFSET,
                ip_bits.to_le_bytes().to_vec(),
            )));
        }

        let config = RpcProgramAccountsConfig {
            filters: Some(filters),
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                ..Default::default()
            },
            ..Default::default()
        };

        let accounts: Vec<(Pubkey, Account)> = connection
            .get_program_accounts_with_config(&reservation::ID, config)
            .await?;

        if accounts.is_empty() {
            println!("No client seats found.");
            return Ok(());
        }

        // Parse all seats.
        let parsed_seats: Vec<_> = accounts
            .iter()
            .filter_map(|(seat_key, account)| {
                let (device_key, client_ip, tenure, _, _) =
                    state::parse_client_seat(&account.data)?;
                Some((*seat_key, device_key, client_ip, tenure))
            })
            .collect();

        // Fetch escrow balances.
        let (escrow_balances, filtered_seats) = if let Some(ref authority) = self.withdraw_authority
        {
            let escrow_keys: Vec<Pubkey> = parsed_seats
                .iter()
                .map(|(seat_key, _, _, _)| {
                    state::find_payment_escrow_address(seat_key, authority).0
                })
                .collect();
            let escrow_accounts = connection.try_fetch_multiple_accounts(&escrow_keys).await?;

            let mut balances: HashMap<Pubkey, u64> = HashMap::new();
            let mut matching_seats = Vec::new();
            for (seat, account) in parsed_seats.into_iter().zip(escrow_accounts.into_iter()) {
                if let Some((seat_key, _, balance)) = state::parse_payment_escrow(&account.data) {
                    balances.insert(seat_key, balance);
                    matching_seats.push(seat);
                }
            }
            (balances, matching_seats)
        } else {
            let escrow_disc_bytes = borsh::to_vec(&state::PAYMENT_ESCROW_DISCRIMINATOR)
                .expect("discriminator serialization");
            let escrow_config = RpcProgramAccountsConfig {
                filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                    0,
                    escrow_disc_bytes,
                ))]),
                account_config: RpcAccountInfoConfig {
                    encoding: Some(UiAccountEncoding::Base64),
                    ..Default::default()
                },
                ..Default::default()
            };
            let escrow_accounts: Vec<(Pubkey, Account)> = connection
                .get_program_accounts_with_config(&reservation::ID, escrow_config)
                .await?;

            let mut balances: HashMap<Pubkey, u64> = HashMap::new();
            for (_, account) in &escrow_accounts {
                if let Some((seat_key, _, balance)) = state::parse_payment_escrow(&account.data) {
                    balances.insert(seat_key, balance);
                }
            }
            (balances, parsed_seats)
        };

        if filtered_seats.is_empty() {
            println!("No client seats found.");
            return Ok(());
        }

        // Collect unique device keys.
        let unique_devices: Vec<Pubkey> = filtered_seats
            .iter()
            .map(|(_, device_key, _, _)| *device_key)
            .collect::<std::collections::HashSet<_>>()
            .into_iter()
            .collect();

        // Resolve device codes from DZ Ledger (best-effort).
        let device_codes: HashMap<Pubkey, String> = {
            let dz_connection = DoubleZeroLedgerConnection::from(network_env);
            let dz_accounts = dz_connection.get_multiple_accounts(&unique_devices).await;
            dz_accounts
                .unwrap_or_default()
                .into_iter()
                .zip(unique_devices.iter())
                .filter_map(|(account, key)| {
                    let device = Device::try_from(account?.data.as_slice()).ok()?;
                    Some((*key, device.code))
                })
                .collect()
        };

        // Fetch epoch pricing per device.
        let device_prices = fetch_device_prices(&connection, &unique_devices).await?;

        // Build rows.
        let mut rows: Vec<SeatRow> = filtered_seats
            .iter()
            .map(|(seat_key, device_key, client_ip, tenure)| {
                let device_code = device_codes
                    .get(device_key)
                    .cloned()
                    .unwrap_or_else(|| device_key.to_string());

                let balance = escrow_balances.get(seat_key).copied().unwrap_or(0);
                let escrow_usdc = format!("{:.2}", balance as f64 / 1_000_000.0);
                let price = device_prices.get(device_key).copied().unwrap_or(0);
                let est_epochs_paid = if price > 0 {
                    format!("~{}", balance / price)
                } else {
                    "N/A".to_string()
                };

                SeatRow {
                    device_code,
                    client_ip: *client_ip,
                    tenure: *tenure,
                    escrow_usdc,
                    est_epochs_paid,
                }
            })
            .collect();

        rows.sort_by(|a, b| {
            a.device_code
                .cmp(&b.device_code)
                .then(a.client_ip.cmp(&b.client_ip))
        });

        println!("{} seat(s) found:\n", rows.len());

        let mut table = Table::new(rows);
        table.with(Style::markdown());
        println!("{table}");

        Ok(())
    }
}

/// Fetch the current epoch price (base + premium, in micro-USDC) for each device.
async fn fetch_device_prices(
    connection: &SolanaConnection,
    device_keys: &[Pubkey],
) -> Result<HashMap<Pubkey, u64>> {
    if device_keys.is_empty() {
        return Ok(HashMap::new());
    }

    // Fetch DeviceHistory accounts.
    let dh_keys: Vec<Pubkey> = device_keys
        .iter()
        .map(|dk| state::find_device_history_address(dk).0)
        .collect();
    let dh_accounts = connection.try_fetch_multiple_accounts(&dh_keys).await?;

    // Parse device infos and collect unique exchange keys.
    let mut device_infos: Vec<(Pubkey, Pubkey, i16)> = Vec::new();
    let mut exchange_keys_set = std::collections::HashSet::new();
    for account in &dh_accounts {
        if let Some(info) = state::parse_device_history(&account.data) {
            exchange_keys_set.insert(info.exchange_key);
            device_infos.push((info.device_key, info.exchange_key, info.current_premium));
        }
    }

    // Fetch MetroHistory accounts.
    let exchange_keys: Vec<Pubkey> = exchange_keys_set.into_iter().collect();
    let mh_keys: Vec<Pubkey> = exchange_keys
        .iter()
        .map(|ek| state::find_metro_history_address(ek).0)
        .collect();
    let mh_accounts = connection.try_fetch_multiple_accounts(&mh_keys).await?;

    let mut metro_prices: HashMap<Pubkey, u16> = HashMap::new();
    for account in &mh_accounts {
        if let Some(info) = state::parse_metro_history(&account.data) {
            metro_prices.insert(info.exchange_key, info.current_usdc_price);
        }
    }

    // Compute total price per device.
    let mut prices = HashMap::new();
    for (device_key, exchange_key, premium) in &device_infos {
        if let Some(&base) = metro_prices.get(exchange_key) {
            let total = (base as i32 + *premium as i32).max(0) as u64;
            prices.insert(*device_key, total);
        }
    }

    Ok(prices)
}
