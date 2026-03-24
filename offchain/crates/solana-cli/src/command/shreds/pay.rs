use std::net::Ipv4Addr;

use anyhow::{Result, bail};
use clap::Args;
use doublezero_serviceability::{pda::get_user_pda, state::user::UserType};
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    shred_subscription::{
        ID,
        instruction::{
            ShredSubscriptionInstructionData,
            account::{
                FundPaymentEscrowUsdcAccounts, InitializeClientSeatAccounts,
                InitializePaymentEscrowAccounts, RequestInstantSeatAllocationAccounts,
            },
        },
        state,
    },
    try_build_instruction,
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};
use spl_associated_token_account_interface::address::get_associated_token_address;

use super::{make_dz_connection, serviceability_program_id};
use crate::command::try_prompt_proceed_confirmation;

/// Warn if less than 10% of the current Solana epoch remains.
const EPOCH_REMAINING_WARNING_THRESHOLD: f64 = 0.10;

/// Solana's target slot duration in seconds. Actual varies with network
/// conditions; used only for approximate display estimates (prefixed with `~`).
const SLOT_DURATION_SECS: f64 = 0.4;

/// Inputs for the epoch-remaining warning check, separated from I/O for testability.
struct EpochWarningInput {
    accept_partial_epoch: bool,
    dry_run: bool,
    seat_active_this_epoch: bool,
    slot_index: u64,
    slots_in_epoch: u64,
}

/// Returns `true` when the client seat already has an active allocation
/// (`active_epoch > 0`). Re-funding an active seat only needs to top up the
/// escrow — requesting a new instant allocation would fail onchain because the
/// seat is already counted against the device's available capacity.
fn is_seat_already_active(seat_data: Option<&[u8]>) -> bool {
    seat_data
        .and_then(state::parse_client_seat)
        .map(|(_, _, _, _, active_epoch)| active_epoch > 0)
        .unwrap_or(false)
}

/// Returns `Some(prompt_message)` if the user should be warned about paying late
/// in the epoch, or `None` if no warning is needed.
fn epoch_warning_prompt(input: &EpochWarningInput) -> Option<String> {
    if input.accept_partial_epoch || input.dry_run || input.seat_active_this_epoch {
        return None;
    }

    if input.slot_index >= input.slots_in_epoch {
        return None;
    }

    let remaining_pct =
        (input.slots_in_epoch - input.slot_index) as f64 / input.slots_in_epoch as f64;

    if remaining_pct >= EPOCH_REMAINING_WARNING_THRESHOLD {
        return None;
    }

    let remaining_secs = (input.slots_in_epoch - input.slot_index) as f64 * SLOT_DURATION_SECS;
    let total_secs = input.slots_in_epoch as f64 * SLOT_DURATION_SECS;

    Some(format!(
        "Only {:.1}% of the current Solana epoch remains ({} of {}).\n  \
         Your seat will be allocated immediately, but covers only the remaining {} of this epoch.\n  \
         A separate payment for the next epoch will be deducted in {} when this epoch ends",
        remaining_pct * 100.0,
        format_duration(remaining_secs),
        format_duration(total_secs),
        format_duration(remaining_secs),
        format_duration(remaining_secs),
    ))
}

/*
   doublezero-solana shreds pay \
       --device <PUBKEY> | --device-code <CODE> \
       --client-ip <IP> --amount <USDC_DECIMAL>
*/

#[derive(Debug, Args)]
pub struct PayCommand {
    #[command(flatten)]
    device_args: super::DeviceArgs,
    /// Client IPv4 address
    #[arg(long)]
    client_ip: Ipv4Addr,
    /// Amount of USDC to fund (in decimal, e.g. 1.5 = 1_500_000 micro-USDC)
    #[arg(long)]
    amount: f64,
    /// USDC mint (defaults to mainnet USDC)
    #[arg(long, hide = true)]
    usdc_mint: Option<Pubkey>,
    /// Source USDC token account (defaults to payer's ATA)
    #[arg(long)]
    source_token_account: Option<Pubkey>,
    /// Skip the epoch-remaining warning prompt (for batch/multi-seat workflows)
    #[arg(long)]
    accept_partial_epoch: bool,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl PayCommand {
    pub async fn try_into_execute(self, dz_ledger_url: Option<String>) -> Result<()> {
        let wallet = Wallet::try_from(self.solana_payer_options)?;
        let wallet_key = wallet.pubkey();

        println!("Shred subscription - Pay");

        let network_env = wallet.connection.try_network_environment().await?;
        println!("Connected to Solana: {network_env:?}");

        let device = self
            .device_args
            .resolve(network_env, &dz_ledger_url)
            .await?;
        let client_ip_bits = u32::from(self.client_ip);

        // Best-effort check: verify this client IP doesn't already have a Multicast
        // user on serviceability. If so, the shred oracle will fail to create a new
        // subscribe user at settlement time. This is not enforced on-chain.
        if let Ok(svc_program_id) = serviceability_program_id(network_env) {
            let dz_connection = make_dz_connection(&dz_ledger_url, network_env);
            let (user_pda, _) = get_user_pda(&svc_program_id, &self.client_ip, UserType::Multicast);
            if let Ok(Some(_)) = dz_connection
                .get_account_with_commitment(&user_pda, CommitmentConfig::confirmed())
                .await
                .map(|r| r.value)
            {
                bail!(
                    "Client IP {} already has an active multicast user on serviceability. \
                     Disconnect first (doublezero disconnect) before purchasing a \
                     shred subscription.",
                    self.client_ip,
                );
            }
        }

        // Derive PDAs.
        let (client_seat_key, seat_bump) = state::find_client_seat_address(&device, client_ip_bits);
        let (escrow_key, escrow_bump) =
            state::find_payment_escrow_address(&client_seat_key, &wallet_key);

        // Check which accounts already exist on-chain.
        let accounts = wallet
            .connection
            .get_multiple_accounts(&[client_seat_key, escrow_key])
            .await
            .unwrap();
        let seat_exists = accounts[0].is_some();
        let escrow_exists = accounts[1].is_some();

        let seat_already_active =
            is_seat_already_active(accounts[0].as_ref().map(|a| a.data.as_slice()));

        // Epoch-remaining warning: if <10% of the epoch remains, the user is
        // paying full price for a partial epoch. Skip if: flag set, dry-run,
        // or seat is already active for this epoch (re-fund).
        // Single RPC call to avoid race conditions at epoch boundaries.
        match wallet.connection.get_epoch_info().await {
            Ok(epoch_info) => {
                // Use >= (not ==) to handle the unlikely case where active_epoch
                // is ahead of the RPC's reported epoch due to timing.
                let seat_active_this_epoch = if let Some(ref seat_account) = accounts[0] {
                    if let Some((_, _, _, _, active_epoch)) =
                        state::parse_client_seat(&seat_account.data)
                    {
                        active_epoch >= epoch_info.epoch
                    } else {
                        false
                    }
                } else {
                    false
                };

                let input = EpochWarningInput {
                    accept_partial_epoch: self.accept_partial_epoch,
                    dry_run: wallet.dry_run,
                    seat_active_this_epoch,
                    slot_index: epoch_info.slot_index,
                    slots_in_epoch: epoch_info.slots_in_epoch,
                };

                if let Some(prompt) = epoch_warning_prompt(&input) {
                    try_prompt_proceed_confirmation(
                        prompt,
                        "Aborted. Consider waiting for the next epoch to start to get a full epoch of service.".to_string(),
                    )?;
                }
            }
            Err(e) => {
                eprintln!("Warning: could not fetch epoch info: {e}");
            }
        }

        let usdc_mint_key = self.usdc_mint.unwrap_or(*state::USDC_MINT_KEY);

        // Convert decimal USDC to micro-USDC (6 decimals).
        if self.amount <= 0.0 {
            bail!("Amount must be a positive value");
        }
        let amount_micro = (self.amount * 1_000_000.0).round() as u64;

        // Derive the exchange key from the on-chain DeviceHistory account.
        let device_history_key = state::find_device_history_address(&device).0;
        let device_history_account = wallet.connection.get_account(&device_history_key).await?;
        let device_info = state::parse_device_history(&device_history_account.data)
            .ok_or_else(|| anyhow::anyhow!("Failed to parse DeviceHistory account"))?;
        let exchange_key = device_info.exchange_key;

        // Check the current price so the user gets a friendly error instead of
        // an opaque on-chain revert.
        let metro_history_key = state::find_metro_history_address(&exchange_key).0;
        let metro_history_account = wallet.connection.get_account(&metro_history_key).await?;
        if let Some(metro_info) = state::parse_metro_history(&metro_history_account.data) {
            let min_price = (metro_info.current_usdc_price as i32
                + device_info.current_premium as i32)
                .max(0) as u64;
            if amount_micro < min_price {
                let min_usdc = min_price as f64 / 1_000_000.0;
                bail!(
                    "Amount ({:.6} USDC) is below the current price ({:.6} USDC)",
                    self.amount,
                    min_usdc,
                );
            }
        }

        let mut instructions = Vec::new();
        let mut compute_unit_limit = 0u32;

        if !seat_exists {
            let seat_ix = try_build_instruction(
                &ID,
                InitializeClientSeatAccounts::new(&wallet_key, &device, client_ip_bits),
                &ShredSubscriptionInstructionData::InitializeClientSeat {
                    client_ip: client_ip_bits,
                },
            )?;
            instructions.push(seat_ix);
            compute_unit_limit += 50_000 + Wallet::compute_units_for_bump_seed(seat_bump);
        }

        if !escrow_exists {
            let escrow_ix = try_build_instruction(
                &ID,
                InitializePaymentEscrowAccounts::new(&client_seat_key, &wallet_key),
                &ShredSubscriptionInstructionData::InitializePaymentEscrow,
            )?;
            instructions.push(escrow_ix);
            compute_unit_limit += 50_000 + Wallet::compute_units_for_bump_seed(escrow_bump);
        }

        let source_usdc_token_account = self
            .source_token_account
            .unwrap_or_else(|| get_associated_token_address(&wallet_key, &usdc_mint_key));

        let fund_ix = try_build_instruction(
            &ID,
            FundPaymentEscrowUsdcAccounts::new(
                &exchange_key,
                &device,
                client_ip_bits,
                &wallet_key,
                &usdc_mint_key,
                &source_usdc_token_account,
                &wallet_key,
            ),
            &ShredSubscriptionInstructionData::FundPaymentEscrowUsdc(amount_micro),
        )?;
        instructions.push(fund_ix);
        compute_unit_limit += 50_000;

        if !seat_already_active {
            let request_ix = try_build_instruction(
                &ID,
                RequestInstantSeatAllocationAccounts::new(
                    &exchange_key,
                    &device,
                    client_ip_bits,
                    &wallet_key,
                    &wallet_key,
                ),
                &ShredSubscriptionInstructionData::RequestInstantSeatAllocation,
            )?;
            instructions.push(request_ix);
            compute_unit_limit += 50_000;
        }

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
            compute_unit_limit,
        ));

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            println!("Fund escrow ({} USDC): {tx_sig}", self.amount);
            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}

/// Format a duration in seconds as a human-readable approximate string.
fn format_duration(seconds: f64) -> String {
    if seconds >= 3600.0 {
        format!("~{:.1} hours", seconds / 3600.0)
    } else {
        let minutes = (seconds / 60.0).round() as u64;
        if minutes == 1 {
            "~1 minute".to_string()
        } else {
            format!("~{minutes} minutes")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // --- format_duration tests ---

    #[test]
    fn format_duration_hours() {
        assert_eq!(format_duration(7200.0), "~2.0 hours");
        assert_eq!(format_duration(5400.0), "~1.5 hours");
        assert_eq!(format_duration(3600.0), "~1.0 hours");
    }

    #[test]
    fn format_duration_minutes() {
        assert_eq!(format_duration(600.0), "~10 minutes");
        assert_eq!(format_duration(90.0), "~2 minutes");
        assert_eq!(format_duration(30.0), "~1 minute");
    }

    #[test]
    fn format_duration_zero() {
        assert_eq!(format_duration(0.0), "~0 minutes");
    }

    #[test]
    fn format_duration_boundary() {
        // Just under an hour -> minutes
        assert_eq!(format_duration(3599.0), "~60 minutes");
        // Exactly an hour -> hours
        assert_eq!(format_duration(3600.0), "~1.0 hours");
    }

    // --- epoch_warning_prompt tests (behavior matrix) ---

    fn make_input(remaining_pct: f64) -> EpochWarningInput {
        let slots_in_epoch = 432_000; // typical Solana epoch
        let slot_index = ((1.0 - remaining_pct) * slots_in_epoch as f64) as u64;
        EpochWarningInput {
            accept_partial_epoch: false,
            dry_run: false,
            seat_active_this_epoch: false,
            slot_index,
            slots_in_epoch,
        }
    }

    #[test]
    fn no_warning_when_epoch_has_plenty_remaining() {
        let input = make_input(0.50); // 50% remaining
        assert!(epoch_warning_prompt(&input).is_none());
    }

    #[test]
    fn no_warning_at_exactly_threshold() {
        let input = make_input(0.10); // exactly 10%
        assert!(epoch_warning_prompt(&input).is_none());
    }

    #[test]
    fn warning_when_below_threshold() {
        let input = make_input(0.05); // 5% remaining
        let prompt = epoch_warning_prompt(&input).expect("should warn");
        assert!(prompt.contains("5.0%"));
        assert!(prompt.contains("Your seat will be allocated immediately"));
        assert!(prompt.contains("A separate payment for the next epoch"));
    }

    #[test]
    fn no_warning_when_accept_partial_epoch_set() {
        let mut input = make_input(0.05);
        input.accept_partial_epoch = true;
        assert!(epoch_warning_prompt(&input).is_none());
    }

    #[test]
    fn no_warning_when_dry_run() {
        let mut input = make_input(0.05);
        input.dry_run = true;
        assert!(epoch_warning_prompt(&input).is_none());
    }

    #[test]
    fn no_warning_when_seat_already_active() {
        let mut input = make_input(0.05);
        input.seat_active_this_epoch = true;
        assert!(epoch_warning_prompt(&input).is_none());
    }

    #[test]
    fn no_warning_when_slots_in_epoch_zero() {
        let input = EpochWarningInput {
            accept_partial_epoch: false,
            dry_run: false,
            seat_active_this_epoch: false,
            slot_index: 0,
            slots_in_epoch: 0,
        };
        assert!(epoch_warning_prompt(&input).is_none());
    }

    #[test]
    fn warning_message_contains_time_estimates() {
        let input = make_input(0.05);
        let prompt = epoch_warning_prompt(&input).expect("should warn");
        // 5% of 432000 slots = 21600 slots * 0.4s = 8640s = 2.4 hours
        assert!(prompt.contains("~2.4 hours"));
        // Total epoch: 432000 * 0.4s = 172800s = 48.0 hours
        assert!(prompt.contains("~48.0 hours"));
    }

    #[test]
    fn warning_near_epoch_end() {
        // ~1% remaining
        let input = make_input(0.01);
        let prompt = epoch_warning_prompt(&input).expect("should warn");
        assert!(prompt.contains("1.0%"));
    }

    // --- Additional coverage: format_duration edge cases ---

    // 29 s rounds to 0, 30 s rounds to 1. The existing test_minutes test
    // covers 30 s ("~1 minute") but never calls the sub-minute path.
    #[test]
    fn format_duration_sub_minute_rounds_to_zero() {
        // 29 s → 0.483 minutes → rounds to 0
        assert_eq!(format_duration(29.0), "~0 minutes");
    }

    #[test]
    fn format_duration_fractional_seconds_rounds_correctly() {
        // 89.9 s → 1.498 minutes → rounds to 1
        assert_eq!(format_duration(89.9), "~1 minute");
        // 90.1 s → 1.502 minutes → rounds to 2
        assert_eq!(format_duration(90.1), "~2 minutes");
    }

    // The spec says format_duration is used exclusively for display of slot-based
    // time estimates (non-negative). Confirm zero-slot-remaining produces "~0 minutes"
    // rather than panicking.
    #[test]
    fn format_duration_exactly_zero_slots_remaining() {
        // 0 slots * 0.4 s = 0.0 s
        assert_eq!(format_duration(0.0 * SLOT_DURATION_SECS), "~0 minutes");
    }

    // --- Additional coverage: epoch_warning_prompt edge cases ---

    // Spec row: "not passed | no | no | >= 10% → Proceed silently".
    // The 10% boundary is already tested; cover 10.0001% to be sure the
    // threshold is exclusive-above (>= 0.10 means no warning).
    #[test]
    fn no_warning_just_above_threshold() {
        // 10.01% remaining — should NOT warn
        let slots_in_epoch = 432_000u64;
        let slot_index = ((1.0 - 0.1001_f64) * slots_in_epoch as f64) as u64;
        let input = EpochWarningInput {
            accept_partial_epoch: false,
            dry_run: false,
            seat_active_this_epoch: false,
            slot_index,
            slots_in_epoch,
        };
        assert!(epoch_warning_prompt(&input).is_none());
    }

    // Spec row: "not passed | no | no | < 10% → Show warning + prompt".
    // Test the boundary from the other side: just under 10%.
    #[test]
    fn warning_just_below_threshold() {
        // 9.99% remaining — should warn
        let slots_in_epoch = 432_000u64;
        let slot_index = ((1.0 - 0.0999_f64) * slots_in_epoch as f64) as u64;
        let input = EpochWarningInput {
            accept_partial_epoch: false,
            dry_run: false,
            seat_active_this_epoch: false,
            slot_index,
            slots_in_epoch,
        };
        assert!(epoch_warning_prompt(&input).is_some());
    }

    // When slot_index == slots_in_epoch (epoch boundary), the guard returns
    // None to avoid u64 underflow. This is safe — the epoch is transitioning.
    #[test]
    fn no_warning_when_slot_index_equals_slots_in_epoch() {
        let slots_in_epoch = 432_000u64;
        let input = EpochWarningInput {
            accept_partial_epoch: false,
            dry_run: false,
            seat_active_this_epoch: false,
            slot_index: slots_in_epoch,
            slots_in_epoch,
        };
        assert!(epoch_warning_prompt(&input).is_none());
    }

    // When slot_index > slots_in_epoch (transient RPC timing), the guard
    // returns None to prevent u64 underflow panic.
    #[test]
    fn no_warning_when_slot_index_exceeds_slots_in_epoch() {
        let input = EpochWarningInput {
            accept_partial_epoch: false,
            dry_run: false,
            seat_active_this_epoch: false,
            slot_index: 432_001,
            slots_in_epoch: 432_000,
        };
        assert!(epoch_warning_prompt(&input).is_none());
    }

    // Verify that exactly 1 slot remaining triggers a warning and produces
    // a minutes-based (not hours-based) time estimate.
    #[test]
    fn warning_with_one_slot_remaining() {
        let slots_in_epoch = 432_000u64;
        let input = EpochWarningInput {
            accept_partial_epoch: false,
            dry_run: false,
            seat_active_this_epoch: false,
            slot_index: slots_in_epoch - 1, // 1 slot = 0.4 s remaining
            slots_in_epoch,
        };
        let prompt = epoch_warning_prompt(&input).expect("should warn");
        // 1 slot * 0.4 s = 0.4 s → rounds to "~0 minutes"
        assert!(prompt.contains("~0 minutes"));
        // Total epoch is hours, not minutes
        assert!(prompt.contains("~48.0 hours"));
    }

    // Spec row: "passed | any | any | any → Proceed silently, no warning".
    // Test that accept_partial_epoch suppresses the warning even when all
    // other flags would also suppress it (ensure the OR-of-suppressors
    // never produces a warning regardless of combination).
    #[test]
    fn no_warning_when_all_suppress_flags_set_simultaneously() {
        let mut input = make_input(0.01); // deep in warning zone
        input.accept_partial_epoch = true;
        input.dry_run = true;
        input.seat_active_this_epoch = true;
        assert!(epoch_warning_prompt(&input).is_none());
    }

    // accept_partial_epoch + dry_run (two flags, seat not active)
    #[test]
    fn no_warning_accept_partial_and_dry_run_combined() {
        let mut input = make_input(0.01);
        input.accept_partial_epoch = true;
        input.dry_run = true;
        assert!(epoch_warning_prompt(&input).is_none());
    }

    // dry_run + seat_active (two flags, accept_partial not set)
    #[test]
    fn no_warning_dry_run_and_seat_active_combined() {
        let mut input = make_input(0.01);
        input.dry_run = true;
        input.seat_active_this_epoch = true;
        assert!(epoch_warning_prompt(&input).is_none());
    }

    // Verify the warning content uses minutes (not hours) for the remaining-time
    // fields when less than an hour remains, even though the total epoch time is
    // still rendered in hours.
    // 1% of a 432,000-slot epoch = 4,320 slots * 0.4 s = 1,728 s ≈ 28.8 minutes
    #[test]
    fn warning_near_epoch_end_shows_minutes_for_remaining_time() {
        let input = make_input(0.01); // 1% remaining → ~29 minutes
        let prompt = epoch_warning_prompt(&input).expect("should warn");
        // The remaining-time estimate must appear as minutes.
        assert!(
            prompt.contains("~29 minutes"),
            "expected '~29 minutes' for remaining time, got: {prompt}"
        );
        // The total epoch estimate is still rendered in hours.
        assert!(
            prompt.contains("~48.0 hours"),
            "expected '~48.0 hours' for total epoch, got: {prompt}"
        );
    }

    // Verify the warning message contains the percentage, both time estimates
    // (remaining and total), and the key user-facing sentences — a complete
    // structural check rather than spot-checks.
    #[test]
    fn warning_message_complete_structure() {
        let input = make_input(0.05); // 5% remaining
        let prompt = epoch_warning_prompt(&input).unwrap();
        assert!(prompt.contains("5.0%"), "missing percentage");
        assert!(
            prompt.contains("~2.4 hours"),
            "missing remaining time estimate"
        );
        assert!(
            prompt.contains("~48.0 hours"),
            "missing total epoch time estimate"
        );
        assert!(
            prompt.contains("Your seat will be allocated immediately"),
            "missing allocation sentence"
        );
        assert!(
            prompt.contains("covers only the remaining"),
            "missing coverage sentence"
        );
        assert!(
            prompt.contains("A separate payment for the next epoch"),
            "missing next-epoch payment sentence"
        );
        assert!(
            prompt.contains("when this epoch ends"),
            "missing epoch-end clause"
        );
    }

    // Verify that a non-standard (small) epoch size still computes correctly.
    // Solana devnet and localnet use smaller epoch sizes.
    #[test]
    fn warning_with_small_epoch_size() {
        // 8-slot devnet-like epoch, 1 slot remaining = 12.5%, below threshold
        let slots_in_epoch = 8u64;
        // 7/8 = 87.5% used → 12.5% remaining: above threshold, no warning
        let input_above = EpochWarningInput {
            accept_partial_epoch: false,
            dry_run: false,
            seat_active_this_epoch: false,
            slot_index: 7,
            slots_in_epoch,
        };
        assert!(epoch_warning_prompt(&input_above).is_none());

        // 8/8 used → slot_index == slots_in_epoch: guard returns None (epoch boundary)
        let input_boundary = EpochWarningInput {
            accept_partial_epoch: false,
            dry_run: false,
            seat_active_this_epoch: false,
            slot_index: 8,
            slots_in_epoch,
        };
        assert!(epoch_warning_prompt(&input_boundary).is_none());
    }

    // Verify that slots_in_epoch = 1 (pathological minimum non-zero epoch)
    // does not panic and returns None because 100% - 0% = 100% > 10%.
    #[test]
    fn no_warning_with_single_slot_epoch_at_start() {
        let input = EpochWarningInput {
            accept_partial_epoch: false,
            dry_run: false,
            seat_active_this_epoch: false,
            slot_index: 0,
            slots_in_epoch: 1,
        };
        // 1/1 = 100% remaining → no warning
        assert!(epoch_warning_prompt(&input).is_none());
    }

    // --- is_seat_already_active tests ---

    /// Build a minimal ClientSeat byte buffer with the given active_epoch.
    fn make_seat_data(active_epoch: u64) -> Vec<u8> {
        // Minimum valid size: ACTIVE_EPOCH_OFFSET + 8 = 64 + 8 = 72 bytes
        // (DISCRIMINATOR_LEN = 8, offsets are relative to that)
        let mut data = vec![0u8; 72];
        // Write active_epoch at offset 64 (DISCRIMINATOR_LEN + 56).
        data[64..72].copy_from_slice(&active_epoch.to_le_bytes());
        data
    }

    #[test]
    fn seat_active_when_active_epoch_nonzero() {
        let data = make_seat_data(5);
        assert!(is_seat_already_active(Some(&data)));
    }

    #[test]
    fn seat_not_active_when_active_epoch_zero() {
        let data = make_seat_data(0);
        assert!(!is_seat_already_active(Some(&data)));
    }

    #[test]
    fn seat_not_active_when_no_account() {
        assert!(!is_seat_already_active(None));
    }

    #[test]
    fn seat_not_active_when_data_too_short() {
        let short_data = vec![0u8; 10];
        assert!(!is_seat_already_active(Some(&short_data)));
    }
}
