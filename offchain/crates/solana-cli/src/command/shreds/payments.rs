use std::net::Ipv4Addr;

use anyhow::Result;
use borsh::BorshDeserialize;
use clap::Args;
use doublezero_solana_client_tools::rpc::SolanaConnectionOptions;
use doublezero_solana_sdk::shred_subscription::{
    self as shred_subscription, instruction::ShredSubscriptionInstructionData, state,
};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_client::GetConfirmedSignaturesForAddress2Config,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig, RpcTransactionConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{account::Account, pubkey::Pubkey, signature::Signature};
use solana_transaction_status_client_types::UiTransactionEncoding;
use tabled::{Table, Tabled, settings::Style};

/*
   doublezero-solana shreds payments \
       --device <PUBKEY> | --device-code <CODE> \
       --client-ip <IP>
*/

#[derive(Debug, Args)]
pub struct PaymentsCommand {
    #[command(flatten)]
    device_args: super::DeviceArgs,

    /// Client IPv4 address.
    #[arg(long)]
    client_ip: Ipv4Addr,

    /// Maximum number of transactions to inspect.
    #[arg(long, default_value = "50")]
    limit: usize,

    #[arg(long)]
    json: bool,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

#[derive(Debug, Tabled, serde::Serialize)]
struct PaymentRow {
    #[tabled(rename = "Event")]
    event: String,
    #[tabled(rename = "Amount (USDC)")]
    amount: String,
    #[tabled(rename = "Date/Time")]
    datetime: String,
    #[tabled(rename = "Balance (USDC)")]
    balance: String,
}

#[derive(Debug)]
struct PaymentEvent {
    event_type: EventType,
    /// Signed amount in micro-USDC (positive = credit, negative = debit).
    amount_micro: i64,
    block_time: Option<i64>,
}

#[derive(Debug)]
#[allow(dead_code)] // Payment variant reserved for phase 2 (oracle settlement debits).
enum EventType {
    Funded,
    Payment,
    Withdrawal,
}

impl std::fmt::Display for EventType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            EventType::Funded => write!(f, "funded"),
            EventType::Payment => write!(f, "payment"),
            EventType::Withdrawal => write!(f, "withdrawal"),
        }
    }
}

impl PaymentsCommand {
    pub async fn try_into_execute(self, dz_ledger_url: Option<String>) -> Result<()> {
        let moniker_env = self.connection_options.moniker_env();
        let connection = self.connection_options.into_shred_subscription_connection();
        let network_env = match moniker_env {
            Some(env) => env,
            None => connection.try_network_environment().await?,
        };

        let device = self
            .device_args
            .resolve(network_env, &dz_ledger_url)
            .await?;

        let client_ip_bits = u32::from(self.client_ip);
        let (client_seat_key, _) = state::find_client_seat_address(&device, client_ip_bits);

        // Discover all escrows for this seat.
        let escrow_disc_bytes = borsh::to_vec(&state::PAYMENT_ESCROW_DISCRIMINATOR)
            .expect("discriminator serialization");
        let config = RpcProgramAccountsConfig {
            filters: Some(vec![
                RpcFilterType::Memcmp(Memcmp::new_raw_bytes(0, escrow_disc_bytes)),
                RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                    state::PAYMENT_ESCROW_SEAT_OFFSET,
                    client_seat_key.to_bytes().to_vec(),
                )),
            ]),
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                ..Default::default()
            },
            ..Default::default()
        };

        let escrow_accounts: Vec<(Pubkey, Account)> = connection
            .get_program_accounts_with_config(&shred_subscription::ID, config)
            .await?;

        if escrow_accounts.is_empty() {
            println!("No payment escrows found for this seat.");
            return Ok(());
        }

        let escrow_keys: Vec<Pubkey> = escrow_accounts.iter().map(|(key, _)| *key).collect();

        // Fetch transaction history for each escrow.
        let mut events: Vec<PaymentEvent> = Vec::new();

        for escrow_key in &escrow_keys {
            let sigs_config = GetConfirmedSignaturesForAddress2Config {
                limit: Some(self.limit),
                commitment: Some(CommitmentConfig::confirmed()),
                ..Default::default()
            };

            let signatures = connection
                .get_signatures_for_address_with_config(escrow_key, sigs_config)
                .await?;

            for sig_info in &signatures {
                // Skip failed transactions.
                if sig_info.err.is_some() {
                    continue;
                }

                let signature: Signature = sig_info.signature.parse()?;

                let tx_config = RpcTransactionConfig {
                    encoding: Some(UiTransactionEncoding::Base64),
                    commitment: Some(CommitmentConfig::confirmed()),
                    max_supported_transaction_version: Some(0),
                };

                let tx_response = connection
                    .get_transaction_with_config(&signature, tx_config)
                    .await?;

                let versioned_tx = match tx_response.transaction.transaction.decode() {
                    Some(tx) => tx,
                    None => continue,
                };

                let message = versioned_tx.message;
                let account_keys = message.static_account_keys();

                for ix in message.instructions() {
                    let program_id = account_keys
                        .get(ix.program_id_index as usize)
                        .copied()
                        .unwrap_or_default();

                    if program_id != *shred_subscription::ID {
                        continue;
                    }

                    // Check that this instruction touches our escrow account.
                    let touches_escrow = ix.accounts.iter().any(|&idx| {
                        account_keys
                            .get(idx as usize)
                            .map(|k| escrow_keys.contains(k))
                            .unwrap_or(false)
                    });

                    if !touches_escrow {
                        continue;
                    }

                    match ShredSubscriptionInstructionData::try_from_slice(&ix.data) {
                        Ok(ShredSubscriptionInstructionData::FundPaymentEscrowUsdc(amount)) => {
                            events.push(PaymentEvent {
                                event_type: EventType::Funded,
                                amount_micro: amount as i64,
                                block_time: tx_response.block_time,
                            });
                        }
                        // TODO: ClosePaymentEscrow (withdrawal) — the actual
                        // refunded amount is in the tx log message "Withdrew {}
                        // USDC from payment escrow to refund account". Parse that
                        // to get the negative amount. Without it, we can't derive
                        // the correct withdrawal amount from the running balance
                        // alone because oracle debits are not yet tracked.
                        Ok(ShredSubscriptionInstructionData::ClosePaymentEscrow) => {}
                        // These instructions touch the escrow account but don't
                        // move funds — they appear in the same tx as fund/close.
                        //
                        // NOTE: the `InitializeValidatorPublisherRewards` and
                        // `ConfigureValidatorPublisherRewards` variants were
                        // previously listed here, but their account lists do
                        // not reference the escrow PDA so the `touches_escrow`
                        // pre-filter above already excludes them. A sibling
                        // task audits the rest of this listing for the same
                        // reason — the wildcard arm below makes the match
                        // robust to future variants in either direction.
                        Ok(
                            ShredSubscriptionInstructionData::InitializePaymentEscrow
                            | ShredSubscriptionInstructionData::InitializeClientSeat { .. }
                            | ShredSubscriptionInstructionData::RequestInstantSeatAllocation
                            | ShredSubscriptionInstructionData::RequestInstantSeatWithdrawal
                            | ShredSubscriptionInstructionData::RequestProratedInstantSeatWithdrawal
                            | ShredSubscriptionInstructionData::SetValidatorClientRewardsProportion(
                                _,
                            )
                            | ShredSubscriptionInstructionData::InitializeClaimHolding(_)
                            | ShredSubscriptionInstructionData::ClaimValidatorClientRewards(_)
                            | ShredSubscriptionInstructionData::CheckCliVersion { .. },
                        ) => {}
                        Ok(_) => {}
                        // TODO: oracle instructions (BatchAllocateSeats,
                        // InstantAllocateSeat) debit the escrow. Their
                        // discriminators are not in the offchain SDK. The debit
                        // amount is in the tx log "Escrow balance: {}" (the
                        // post-debit balance). Parse that and compute the delta
                        // from the running balance to get the negative amount.
                        Err(_) => {}
                    }
                }
            }
        }

        if events.is_empty() {
            if self.json {
                println!("[]");
            } else {
                println!("No payment events found.");
            }
            return Ok(());
        }

        // Sort oldest-first by block_time so we can compute running balances.
        events.sort_by_key(|e| e.block_time.unwrap_or(0));

        // Compute running balance and build rows.
        let mut balance: i64 = 0;
        let mut rows: Vec<PaymentRow> = events
            .iter()
            .map(|event| {
                balance += event.amount_micro;

                let amount_str = if event.amount_micro >= 0 {
                    format!("+{:.2}", event.amount_micro as f64 / 1_000_000.0)
                } else {
                    format!("{:.2}", event.amount_micro as f64 / 1_000_000.0)
                };

                let datetime = event
                    .block_time
                    .and_then(|ts| {
                        chrono::DateTime::from_timestamp(ts, 0)
                            .map(|dt| dt.format("%Y-%m-%d %H:%M:%S UTC").to_string())
                    })
                    .unwrap_or_else(|| "—".to_string());

                PaymentRow {
                    event: event.event_type.to_string(),
                    amount: amount_str,
                    datetime,
                    balance: format!("{:.2}", balance as f64 / 1_000_000.0),
                }
            })
            .collect();

        // Reverse for display: most recent first.
        rows.reverse();

        if self.json {
            println!("{}", serde_json::to_string_pretty(&rows)?);
        } else {
            println!(
                "Payment history for seat {} (client IP {}):\n",
                client_seat_key, self.client_ip
            );

            let mut table = Table::new(rows);
            table.with(Style::markdown());
            println!("{table}");
        }

        Ok(())
    }
}
