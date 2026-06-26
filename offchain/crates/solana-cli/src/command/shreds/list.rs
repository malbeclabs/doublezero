use std::{collections::HashMap, net::Ipv4Addr};

use anyhow::Result;
use clap::Args;
use doublezero_serviceability::state::device::Device;
use doublezero_solana_client_tools::{
    payer::try_load_keypair,
    rpc::{SolanaConnection, SolanaConnectionOptions},
};
use doublezero_solana_sdk::shred_subscription::{self, state};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::{account::Account, pubkey::Pubkey, signer::Signer};
use tabled::{Table, Tabled, settings::Style};

use super::make_dz_connection;

/*
   doublezero-solana shred-subscription list [--device <PUBKEY> | --device-code <CODE>]
*/

#[derive(Debug, Args)]
pub struct ListCommand {
    /// Filter seats by device.
    #[command(flatten)]
    device_args: super::DeviceArgs,

    /// Filter seats by funder (withdraw authority). Accepts a public key or a
    /// path to a keypair file. When omitted, defaults to the default keypair's
    /// public key; if no default keypair is found, shows all seats.
    #[arg(long, short = 'k')]
    funder: Option<String>,

    /// Filter seats by client IPv4 address.
    #[arg(long)]
    client_ip: Option<Ipv4Addr>,

    /// Show seats regardless of funder, restricted to those active in the
    /// current subscription epoch (whose `active_epoch >= current_epoch`).
    /// Lapsed seats, whose accounts persist on-chain, are excluded.
    #[arg(long)]
    all: bool,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

/// A parsed client seat: `(seat_key, device_key, client_ip, tenure)`.
type ParsedSeat = (Pubkey, Pubkey, Ipv4Addr, u16);

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
    pub async fn try_into_execute(self, dz_ledger_url: Option<String>) -> Result<()> {
        let moniker_env = self.connection_options.moniker_env();
        let connection = self.connection_options.into_shred_subscription_connection();

        let discriminator_bytes =
            borsh::to_vec(&state::CLIENT_SEAT_DISCRIMINATOR).expect("discriminator serialization");

        let mut filters = vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
            0,
            discriminator_bytes,
        ))];

        // Resolve device filter.
        let network_env = match moniker_env {
            Some(env) => env,
            None => connection.try_network_environment().await?,
        };
        if self.device_args.device.is_some() || self.device_args.device_code.is_some() {
            let device = self
                .device_args
                .resolve(network_env, &dz_ledger_url)
                .await?;
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
            .get_program_accounts_with_config(&shred_subscription::ID, config)
            .await?;

        if accounts.is_empty() {
            println!("No client seats found.");
            return Ok(());
        }

        // Parse all seats. When `--all` is set, also record each seat's
        // active_epoch for the active-seat filter below.
        let mut active_epoch_by_seat: HashMap<Pubkey, u64> = HashMap::new();
        let parsed_seats: Vec<ParsedSeat> = accounts
            .iter()
            .filter_map(|(seat_key, account)| {
                let (device_key, client_ip, tenure, _, active_epoch) =
                    state::parse_client_seat(&account.data)?;
                if self.all {
                    active_epoch_by_seat.insert(*seat_key, active_epoch);
                }
                Some((*seat_key, device_key, client_ip, tenure))
            })
            .collect();

        // Resolve the funder (withdraw authority) filter.
        let funder: Option<Pubkey> = if let Some(ref funder_str) = self.funder {
            if let Ok(pubkey) = funder_str.parse::<Pubkey>() {
                Some(pubkey)
            } else {
                let keypair = try_load_keypair(Some(funder_str.into()))?;
                Some(keypair.pubkey())
            }
        } else if !self.all {
            try_load_keypair(None).ok().map(|kp| kp.pubkey())
        } else {
            None
        };

        // Fetch escrow balances.
        let (escrow_balances, filtered_seats) = if let Some(ref authority) = funder {
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
                .get_program_accounts_with_config(&shred_subscription::ID, escrow_config)
                .await?;

            let mut balances: HashMap<Pubkey, u64> = HashMap::new();
            for (_, account) in &escrow_accounts {
                if let Some((seat_key, _, balance)) = state::parse_payment_escrow(&account.data) {
                    balances.insert(seat_key, balance);
                }
            }
            let matching_seats: Vec<_> = parsed_seats
                .into_iter()
                .filter(|(seat_key, _, _, _)| balances.contains_key(seat_key))
                .collect();
            (balances, matching_seats)
        };

        let filtered_seats = if self.all {
            let current_epoch = connection.get_epoch_info().await?.epoch;
            println!("Active subscription epoch: {current_epoch}\n");
            active_seats(filtered_seats, &active_epoch_by_seat, current_epoch)
        } else {
            filtered_seats
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
            let dz_connection = make_dz_connection(&dz_ledger_url, network_env);
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
                // balance is micro-USDC (1 USDC = 1_000_000), price is whole USDC.
                let est_epochs_paid = if price > 0 {
                    format!("~{}", balance / (price * 1_000_000))
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

fn active_seats(
    seats: Vec<ParsedSeat>,
    active_epoch_by_seat: &HashMap<Pubkey, u64>,
    current_epoch: u64,
) -> Vec<ParsedSeat> {
    seats
        .into_iter()
        .filter(|(seat_key, _, _, _)| {
            active_epoch_by_seat
                .get(seat_key)
                .is_some_and(|&active_epoch| active_epoch >= current_epoch)
        })
        .collect()
}

/// Fetch the current epoch price (base + premium, in whole USDC) for each device.
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

#[cfg(test)]
mod tests {
    use super::*;

    /// Build a seat with the given active_epoch, returning the seat tuple and
    /// its `(seat_key, active_epoch)` entry for the lookup map.
    fn make_seat(active_epoch: u64) -> (ParsedSeat, (Pubkey, u64)) {
        let seat_key = Pubkey::new_unique();
        let seat = (
            seat_key,
            Pubkey::new_unique(),
            Ipv4Addr::new(10, 0, 0, 1),
            1,
        );
        (seat, (seat_key, active_epoch))
    }

    #[test]
    fn active_seats_keeps_current_and_future_epochs() {
        let (lapsed, lapsed_e) = make_seat(4);
        let (current, current_e) = make_seat(5);
        let (ahead, ahead_e) = make_seat(6);
        let seats = vec![lapsed, current, ahead];
        let map = HashMap::from([lapsed_e, current_e, ahead_e]);

        let result = active_seats(seats, &map, 5);

        let keys: Vec<Pubkey> = result.iter().map(|(k, ..)| *k).collect();
        assert_eq!(keys, vec![current.0, ahead.0]);
    }

    #[test]
    fn active_seats_excludes_all_when_subset_is_lapsed() {
        // Regression: a --device subset where every seat has lapsed must not
        // report stale seats as active. The old max()/== logic treated the
        // most-recent lapsed seat as "active" against a stale derived epoch.
        let (s1, e1) = make_seat(3);
        let (s2, e2) = make_seat(4);
        let seats = vec![s1, s2];
        let map = HashMap::from([e1, e2]);

        let result = active_seats(seats, &map, 5);

        assert!(result.is_empty());
    }

    #[test]
    fn active_seats_excludes_seat_missing_from_map() {
        let (seat, _) = make_seat(5);
        let result = active_seats(vec![seat], &HashMap::new(), 5);
        assert!(result.is_empty());
    }
}
