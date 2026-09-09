use std::{io::Write, net::Ipv4Addr};

use anyhow::Result;
use borsh::BorshDeserialize;
use clap::Args;
use doublezero_cli_core::CliContext;
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
use solana_transaction_status_client_types::{
    EncodedTransaction, UiInstruction, UiMessage, UiParsedInstruction, UiTransactionEncoding,
};
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

const LEGACY_FUND_PAYMENT_ESCROW_USDC: [u8; 8] = [111, 6, 96, 2, 121, 92, 68, 147];

fn parse_legacy_fund_payment_escrow_usdc(data: &[u8]) -> Option<u64> {
    if data.len() < 16 || data[..8] != LEGACY_FUND_PAYMENT_ESCROW_USDC {
        return None;
    }
    Some(u64::from_le_bytes(data[8..16].try_into().ok()?))
}

fn payment_events(
    transaction: &EncodedTransaction,
    escrow_keys: &[Pubkey],
    block_time: Option<i64>,
) -> Vec<PaymentEvent> {
    let EncodedTransaction::Json(ui_transaction) = transaction else {
        return Vec::new();
    };
    let UiMessage::Parsed(message) = &ui_transaction.message else {
        return Vec::new();
    };

    let program_id = shred_subscription::ID.to_string();
    let mut events = Vec::new();
    for instruction in &message.instructions {
        let UiInstruction::Parsed(UiParsedInstruction::PartiallyDecoded(instruction)) = instruction
        else {
            continue;
        };
        if instruction.program_id != program_id {
            continue;
        }
        let touches_escrow = instruction
            .accounts
            .iter()
            .any(|account| escrow_keys.iter().any(|key| key.to_string() == *account));
        if !touches_escrow {
            continue;
        }
        let Ok(data) = bs58::decode(&instruction.data).into_vec() else {
            continue;
        };
        if let Some(amount) = parse_legacy_fund_payment_escrow_usdc(&data) {
            events.push(PaymentEvent {
                event_type: EventType::Funded,
                amount_micro: amount as i64,
                block_time,
            });
            continue;
        }

        match ShredSubscriptionInstructionData::try_from_slice(&data) {
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
    events
}

impl PaymentsCommand {
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
            writeln!(out, "No payment escrows found for this seat.")?;
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
                    encoding: Some(UiTransactionEncoding::JsonParsed),
                    commitment: Some(CommitmentConfig::confirmed()),
                    max_supported_transaction_version: Some(1),
                };

                let tx_response = connection
                    .get_transaction_with_config(&signature, tx_config)
                    .await?;

                events.extend(payment_events(
                    &tx_response.transaction.transaction,
                    &escrow_keys,
                    tx_response.block_time,
                ));
            }
        }

        if events.is_empty() {
            if self.json {
                writeln!(out, "[]")?;
            } else {
                writeln!(out, "No payment events found.")?;
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
            writeln!(out, "{}", serde_json::to_string_pretty(&rows)?)?;
        } else {
            writeln!(
                out,
                "Payment history for seat {} (client IP {}):\n",
                client_seat_key, self.client_ip
            )?;

            let mut table = Table::new(rows);
            table.with(Style::markdown());
            writeln!(out, "{table}")?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use serde_json::{Value, json};
    use solana_transaction_status_client_types::EncodedTransactionWithStatusMeta;

    use super::*;

    fn fund_data(amount: u64) -> String {
        let mut data = Vec::from(LEGACY_FUND_PAYMENT_ESCROW_USDC);
        data.extend_from_slice(&amount.to_le_bytes());
        bs58::encode(data).into_string()
    }

    fn response(instructions: Vec<Value>) -> EncodedTransactionWithStatusMeta {
        serde_json::from_value(json!({
            "transaction": {
                "signatures": [Signature::default().to_string()],
                "message": {
                    "accountKeys": [],
                    "recentBlockhash": "11111111111111111111111111111111",
                    "instructions": instructions,
                    "addressTableLookups": null,
                    "transactionConfig": {
                        "computeUnitLimit": 30_000,
                        "heapSize": null,
                        "loadedAccountsDataSizeLimit": 200_000,
                        "priorityFee": null,
                    },
                },
            },
            "meta": {
                "err": null,
                "status": { "Ok": null },
                "fee": 5_000,
                "preBalances": [],
                "postBalances": [],
                "innerInstructions": [],
                "logMessages": [],
                "preTokenBalances": [],
                "postTokenBalances": [],
                "rewards": [],
            },
            "version": 1,
        }))
        .expect("a getTransaction response must deserialize")
    }

    fn fund_instruction(escrow: Pubkey, amount: u64) -> Value {
        json!({
            "programId": shred_subscription::ID.to_string(),
            "accounts": [escrow.to_string()],
            "data": fund_data(amount),
            "stackHeight": null,
        })
    }

    #[test]
    fn reads_a_v1_fund_for_a_known_escrow() {
        let escrow = Pubkey::new_unique();
        let transaction = response(vec![fund_instruction(escrow, 2_000_000)]);

        let events = payment_events(&transaction.transaction, &[escrow], Some(1_700_000_000));

        assert_eq!(events.len(), 1);
        assert!(matches!(events[0].event_type, EventType::Funded));
        assert_eq!(events[0].amount_micro, 2_000_000);
        assert_eq!(events[0].block_time, Some(1_700_000_000));
    }

    #[test]
    fn ignores_a_foreign_program_and_a_fund_for_another_escrow() {
        let escrow = Pubkey::new_unique();
        let other = Pubkey::new_unique();
        let transaction = response(vec![
            json!({
                "programId": Pubkey::new_unique().to_string(),
                "accounts": [escrow.to_string()],
                "data": fund_data(2_000_000),
                "stackHeight": null,
            }),
            fund_instruction(other, 2_000_000),
        ]);

        let events = payment_events(&transaction.transaction, &[escrow], None);

        assert!(events.is_empty());
    }
}
