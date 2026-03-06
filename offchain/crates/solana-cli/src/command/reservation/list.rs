use std::net::Ipv4Addr;

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
    #[tabled(rename = "Seat PDA")]
    seat_pda: String,
    #[tabled(rename = "Device")]
    device: String,
    #[tabled(rename = "Client IP")]
    client_ip: Ipv4Addr,
    #[tabled(rename = "Tenure")]
    tenure: u16,
    #[tabled(rename = "Funded Epoch")]
    funded_epoch: u64,
    #[tabled(rename = "Active Epoch")]
    active_epoch: u64,
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

        // Resolve device filter
        if self.device_args.device.is_some() || self.device_args.device_code.is_some() {
            let network_env = connection.try_network_environment().await?;
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

        // Filter by withdraw authority
        let authority_seat_keys: Option<std::collections::HashSet<Pubkey>> =
            if let Some(ref authority) = self.withdraw_authority {
                let escrow_disc_bytes = borsh::to_vec(&state::PAYMENT_ESCROW_DISCRIMINATOR)
                    .expect("discriminator serialization");
                let escrow_filters = vec![
                    RpcFilterType::Memcmp(Memcmp::new_raw_bytes(0, escrow_disc_bytes)),
                    RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                        state::PAYMENT_ESCROW_AUTHORITY_OFFSET,
                        authority.to_bytes().to_vec(),
                    )),
                ];
                let escrow_config = RpcProgramAccountsConfig {
                    filters: Some(escrow_filters),
                    account_config: RpcAccountInfoConfig {
                        encoding: Some(UiAccountEncoding::Base64),
                        ..Default::default()
                    },
                    ..Default::default()
                };
                let escrow_accounts: Vec<(Pubkey, Account)> = connection
                    .get_program_accounts_with_config(&reservation::ID, escrow_config)
                    .await?;
                let keys = escrow_accounts
                    .iter()
                    .filter_map(|(_, account)| {
                        let (seat_key, _, _) = state::parse_payment_escrow(&account.data)?;
                        Some(seat_key)
                    })
                    .collect();
                Some(keys)
            } else {
                None
            };

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

        let mut rows: Vec<SeatRow> = accounts
            .iter()
            .filter_map(|(seat_key, account)| {
                if let Some(ref allowed) = authority_seat_keys
                    && !allowed.contains(seat_key)
                {
                    return None;
                }
                let (device_key, client_ip, tenure, funded_epoch, active_epoch) =
                    state::parse_client_seat(&account.data)?;
                Some(SeatRow {
                    seat_pda: seat_key.to_string(),
                    device: device_key.to_string(),
                    client_ip,
                    tenure,
                    funded_epoch,
                    active_epoch,
                })
            })
            .collect();

        rows.sort_by(|a, b| a.device.cmp(&b.device).then(a.client_ip.cmp(&b.client_ip)));

        println!("{} seat(s) found:\n", rows.len());

        let mut table = Table::new(rows);
        table.with(Style::markdown());
        println!("{table}");

        Ok(())
    }
}
