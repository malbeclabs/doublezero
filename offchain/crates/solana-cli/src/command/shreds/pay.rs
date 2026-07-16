use std::{io::Write, net::Ipv4Addr};

use anyhow::{Result, bail};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_serviceability::{pda::get_user_pda, state::user::UserType};
use doublezero_solana_client_tools::payer::{TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    environment_usdc_token_mint_key,
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
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_commitment_config::CommitmentConfig;
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::pubkey::Pubkey;
use spl_associated_token_account_interface::address::get_associated_token_address;

use super::{make_dz_connection, serviceability_program_id};

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
    prorated_service_enabled: bool,
    slot_index: u64,
    slots_in_epoch: u64,
}

/// Returns `true` when the client seat already has an active allocation
/// (`tenure_epochs > 0`). Re-funding an active seat only needs to top up the
/// escrow — requesting a new instant allocation would fail onchain because the
/// seat is already counted against the device's available capacity.
///
/// Uses `tenure_epochs` rather than `active_epoch` because `BatchClearTenure`
/// zeros only `tenure_epochs` when a seat loses its allocation (leaving
/// `active_epoch` stale). Checking `active_epoch > 0` would incorrectly
/// treat a cleared seat as active and skip the instant allocation request.
fn is_seat_already_active(seat_data: Option<&[u8]>) -> bool {
    seat_data
        .and_then(state::parse_client_seat)
        .map(|(_, _, tenure_epochs, _, _)| tenure_epochs > 0)
        .unwrap_or(false)
}

/// Returns `Some(prompt_message)` if the user should be warned about paying late
/// in the epoch, or `None` if no warning is needed.
fn epoch_warning_prompt(input: &EpochWarningInput) -> Option<String> {
    if input.accept_partial_epoch
        || input.dry_run
        || input.seat_active_this_epoch
        || input.prorated_service_enabled
    {
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

/// Given raw account data from a `getProgramAccounts` query filtered by
/// client IP, return the device keys of any **active** seats that are NOT on
/// `target_device`. Withdrawn seats (`tenure_epochs == 0`) are excluded
/// because they will never win an auction and are harmless — blocking on
/// them would prevent users from migrating an IP to a new device after
/// withdrawal. An empty vec means no conflict.
/// Whether an existing serviceability User PDA for this IP, owned by
/// `user_owner`, is benign for this `pay` call. The shred oracle's
/// `CreateSubscribeUser` would collide on the User PDA unless the existing
/// owner is either the oracle (legacy top-up / re-sub) or the wallet
/// running this command (self-owned per the new design).
fn user_owner_is_acceptable(
    user_owner: Option<Pubkey>,
    oracle_key: Option<Pubkey>,
    wallet_key: Pubkey,
    operator_key: Pubkey,
    wallet_owns_escrow: bool,
) -> bool {
    let is_shred_oracle_user = oracle_key.zip(user_owner).is_some_and(|(o, u)| o == u);
    // Under the operator-key split the oracle provisions the user owned by the
    // operator key, which may be distinct from the wallet (withdraw authority);
    // a re-subscribe/top-up must accept either as self-owned. When this wallet
    // already owns the seat's payment escrow, the user at this IP was
    // provisioned from that escrow under an operator key chosen at init (not
    // re-supplied on top-ups), so whatever owner it carries is ours too — the
    // cross-device case is caught separately by the other-device seat guard.
    let is_self_owned =
        user_owner == Some(wallet_key) || user_owner == Some(operator_key) || wallet_owns_escrow;
    is_shred_oracle_user || is_self_owned
}

fn other_device_keys_for_ip(
    accounts: &[(Pubkey, solana_sdk::account::Account)],
    target_device: &Pubkey,
) -> Vec<Pubkey> {
    accounts
        .iter()
        .filter_map(|(_, account)| {
            let (device_key, _, tenure_epochs, _, _) = state::parse_client_seat(&account.data)?;
            if device_key != *target_device && tenure_epochs > 0 {
                Some(device_key)
            } else {
                None
            }
        })
        .collect()
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
    /// USDC mint (auto-detected from network: mainnet or development)
    #[arg(long, hide = true)]
    usdc_mint: Option<Pubkey>,
    /// Source USDC token account (defaults to payer's ATA)
    #[arg(long)]
    source_token_account: Option<Pubkey>,
    /// Skip the epoch-remaining warning prompt (for batch/multi-seat workflows)
    #[arg(long)]
    accept_partial_epoch: bool,
    /// Shred oracle pubkey (auto-detected from network; override for local dev)
    #[arg(long, hide = true)]
    shred_oracle_key: Option<Pubkey>,
    /// Serviceability program ID for the multicast user guard (auto-detected; override for e2e)
    #[arg(long, hide = true)]
    serviceability_program_id: Option<Pubkey>,
    /// Operator key that becomes the DoubleZero operational identity
    /// (user.owner / access-pass user_payer). Defaults to the payer wallet,
    /// reproducing today's behavior (owner = the withdraw authority).
    #[arg(long)]
    operator_key: Option<Pubkey>,

    #[command(flatten)]
    write_opts: crate::command::WriteVerbOptions,
}

impl PayCommand {
    pub async fn execute(
        self,
        dz_ledger_url: Option<String>,
        ctx: &CliContext,
        out: &mut impl Write,
    ) -> Result<()> {
        let moniker_env = self.write_opts.connection_options.moniker_env();
        let wallet = crate::command::build_wallet(ctx, self.write_opts)?;
        let wallet_key = wallet.pubkey();
        let operator_key = self.operator_key.unwrap_or(wallet_key);

        writeln!(out, "Shred subscription - Pay")?;

        let network_env =
            crate::command::resolve_network_env(&wallet.connection, moniker_env).await?;
        writeln!(out, "Connected to Solana: {network_env:?}")?;

        let device = self
            .device_args
            .resolve(network_env, &dz_ledger_url)
            .await?;
        let client_ip_bits = u32::from(self.client_ip);

        // Derive PDAs and check which accounts already exist on-chain up front:
        // the multicast-user guard below needs escrow_exists to distinguish an
        // operator-keyed seat this wallet already owns from a genuinely foreign
        // user, and the escrow-init path needs the existing owner to reconcile
        // --operator-key.
        let (client_seat_key, seat_bump) = state::find_client_seat_address(&device, client_ip_bits);
        let (escrow_key, escrow_bump) =
            state::find_payment_escrow_address(&client_seat_key, &wallet_key);
        let (program_config_key, _) = state::find_program_config_address();
        let accounts = wallet
            .connection
            .get_multiple_accounts(&[client_seat_key, escrow_key, program_config_key])
            .await?;
        let seat_exists = accounts[0].is_some();
        let escrow_exists = accounts[1].is_some();
        let prorated_service_enabled = accounts[2]
            .as_ref()
            .is_some_and(|a| state::is_prorated_service_enabled(&a.data));

        // Best-effort check: if this client IP already has a Multicast user on
        // serviceability owned by neither the shred oracle nor the wallet
        // running this command, the shred oracle's CreateSubscribeUser would
        // collide on the User PDA. Both oracle-owned (legacy top-up / re-sub)
        // and self-owned (validator-owned per the new design) are benign. We
        // also remember the existing owner so the escrow-init path can reconcile
        // --operator-key against it.
        let mut existing_user_owner = None;
        let svc_program_id_result = match self.serviceability_program_id {
            Some(id) => Ok(id),
            None => serviceability_program_id(network_env),
        };
        if let Ok(svc_program_id) = svc_program_id_result {
            let oracle_key = self
                .shred_oracle_key
                .or_else(|| super::shred_oracle_key(network_env));

            let dz_connection = make_dz_connection(&dz_ledger_url, network_env);
            let (user_pda, _) = get_user_pda(&svc_program_id, &self.client_ip, UserType::Multicast);
            if let Ok(Some(user_account)) = dz_connection
                .get_account_with_commitment(&user_pda, CommitmentConfig::confirmed())
                .await
                .map(|r| r.value)
            {
                existing_user_owner = if user_account.data.len() >= 33 {
                    Pubkey::try_from(&user_account.data[1..33]).ok()
                } else {
                    None
                };

                if !user_owner_is_acceptable(
                    existing_user_owner,
                    oracle_key,
                    wallet.pubkey(),
                    operator_key,
                    escrow_exists,
                ) {
                    bail!(
                        "Client IP {} already has a multicast user on serviceability \
                         owned by neither the shred oracle nor your wallet. This IP \
                         may be subscribed to another wallet's multicast group. \
                         Disconnect first (doublezero disconnect) before purchasing \
                         a shred subscription.",
                        self.client_ip,
                    );
                }
            }
        }

        // Block if this client IP already has a seat on a DIFFERENT device.
        // The serviceability User PDA is keyed by (IP, user_type) with no
        // device dimension, so two seats for the same IP on different devices
        // causes the oracle to fail with AccountAlreadyInitialized.
        let discriminator_bytes =
            borsh::to_vec(&state::CLIENT_SEAT_DISCRIMINATOR).expect("discriminator serialization");
        let ip_bytes = client_ip_bits.to_le_bytes().to_vec();
        let filters = vec![
            RpcFilterType::Memcmp(Memcmp::new_raw_bytes(0, discriminator_bytes)),
            RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                state::CLIENT_SEAT_CLIENT_IP_OFFSET,
                ip_bytes,
            )),
        ];
        let config = RpcProgramAccountsConfig {
            filters: Some(filters),
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                ..Default::default()
            },
            ..Default::default()
        };
        let existing_seats = wallet
            .connection
            .get_program_accounts_with_config(&ID, config)
            .await?;

        let other_device_keys = other_device_keys_for_ip(&existing_seats, &device);

        if !other_device_keys.is_empty() {
            let device_list = other_device_keys
                .iter()
                .map(|k| k.to_string())
                .collect::<Vec<_>>()
                .join(", ");
            bail!(
                "Client IP {} already has a seat on device {}. \
                 Withdraw from that device first before creating a seat on a new device.",
                self.client_ip,
                device_list,
            );
        }

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
                    prorated_service_enabled,
                    slot_index: epoch_info.slot_index,
                    slots_in_epoch: epoch_info.slots_in_epoch,
                };

                if let Some(prompt) = epoch_warning_prompt(&input) {
                    crate::command::try_prompt_proceed_confirmation(
                        out,
                        &prompt,
                        "Aborted. Consider waiting for the next epoch to start to get a full epoch of service.",
                    )?;
                }
            }
            Err(e) => {
                eprintln!("Warning: could not fetch epoch info: {e}");
            }
        }

        let usdc_mint_key = self
            .usdc_mint
            .unwrap_or(environment_usdc_token_mint_key(network_env));

        // Convert decimal USDC to micro-USDC (6 decimals).
        if self.amount < 0.0 {
            bail!("Amount must be a non-negative value");
        }
        let amount_micro = (self.amount * 1_000_000.0).round() as u64;

        // Derive the exchange key from the on-chain DeviceHistory account.
        let device_history_key = state::find_device_history_address(&device).0;
        let device_history_account = wallet.connection.get_account(&device_history_key).await?;
        let device_info = state::parse_device_history(&device_history_account.data)
            .ok_or_else(|| anyhow::anyhow!("Failed to parse DeviceHistory account"))?;
        let exchange_key = device_info.exchange_key;

        // Check the current price so the user gets a friendly error instead of
        // an opaque on-chain revert. If the seat has a per-seat price
        // override, use that instead of the metro base + device premium.
        let seat_price_override = accounts[0]
            .as_ref()
            .and_then(|a| state::parse_client_seat_price_override(&a.data));

        let metro_history_key = state::find_metro_history_address(&exchange_key).0;
        let metro_history_account = wallet.connection.get_account(&metro_history_key).await?;
        if let Some(metro_info) = state::parse_metro_history(&metro_history_account.data) {
            let min_price = seat_price_override.unwrap_or_else(|| {
                (metro_info.current_usdc_price as i32 + device_info.current_premium as i32).max(0)
                    as u64
                    * 1_000_000
            });
            if amount_micro < min_price {
                let min_usdc = min_price as f64 / 1_000_000.0;
                bail!(
                    "Amount ({:.6} USDC) is below the current price ({:.6} USDC)",
                    self.amount,
                    min_usdc,
                );
            }
        }

        if !seat_already_active
            && device_info.granted_seat_count >= device_info.total_available_seats
        {
            bail!(
                "Device has no available seats ({}/{} granted). Choose another device.",
                device_info.granted_seat_count,
                device_info.total_available_seats,
            );
        }

        let mut instructions = vec![super::build_check_cli_version_instruction()?];
        let mut compute_unit_limit = 5_000u32;

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

        // The operator key is only carried by InitializePaymentEscrow and is
        // immutable once the escrow exists — there is no set-operator-key
        // instruction, and the escrow stores no key the CLI could read back.
        // The provisioned user's owner is the source of truth. A top-up that
        // re-states the same key (or omits the flag) proceeds normally; only a
        // flag that would *change* an already-set operator key is refused, so
        // we don't mislead the caller into thinking the change took effect.
        if escrow_exists && self.operator_key.is_some() && existing_user_owner != self.operator_key
        {
            bail!(
                "This seat's payment escrow already exists; its operator key was set when the \
                 escrow was created and cannot be changed. Omit --operator-key to top up the \
                 existing seat."
            );
        }

        if !escrow_exists {
            let escrow_ix = try_build_instruction(
                &ID,
                InitializePaymentEscrowAccounts::new(&client_seat_key, &wallet_key),
                &ShredSubscriptionInstructionData::InitializePaymentEscrow(operator_key),
            )?;
            instructions.push(escrow_ix);
            writeln!(
                out,
                "Initializing payment escrow with operator key: {operator_key}"
            )?;
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
            writeln!(out, "Fund escrow ({} USDC): {tx_sig}", self.amount)?;
            wallet.write_verbose_output(out, &[tx_sig]).await?;
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
    use solana_sdk::account::Account;

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
            prorated_service_enabled: false,
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
    fn no_warning_when_prorated_service_enabled() {
        // Late in epoch + prorated on → warning is moot because the user
        // only pays for the remaining slots.
        let mut input = make_input(0.02);
        input.prorated_service_enabled = true;
        assert!(epoch_warning_prompt(&input).is_none());
    }

    #[test]
    fn no_warning_when_slots_in_epoch_zero() {
        let input = EpochWarningInput {
            accept_partial_epoch: false,
            dry_run: false,
            seat_active_this_epoch: false,
            prorated_service_enabled: false,
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
            prorated_service_enabled: false,
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
            prorated_service_enabled: false,
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
            prorated_service_enabled: false,
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
            prorated_service_enabled: false,
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
            prorated_service_enabled: false,
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
            prorated_service_enabled: false,
            slot_index: 7,
            slots_in_epoch,
        };
        assert!(epoch_warning_prompt(&input_above).is_none());

        // 8/8 used → slot_index == slots_in_epoch: guard returns None (epoch boundary)
        let input_boundary = EpochWarningInput {
            accept_partial_epoch: false,
            dry_run: false,
            seat_active_this_epoch: false,
            prorated_service_enabled: false,
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
            prorated_service_enabled: false,
            slot_index: 0,
            slots_in_epoch: 1,
        };
        // 1/1 = 100% remaining → no warning
        assert!(epoch_warning_prompt(&input).is_none());
    }

    // --- is_seat_already_active tests ---

    const TENURE_OFFSET: usize = 46; // DISCRIMINATOR_LEN (8) + 38
    const ACTIVE_EPOCH_OFFSET: usize = 64; // DISCRIMINATOR_LEN (8) + 56

    /// Build a minimal ClientSeat byte buffer with the given tenure_epochs
    /// and active_epoch. The buffer must be at least 72 bytes for
    /// `parse_client_seat` to succeed.
    fn make_seat_data_ex(tenure_epochs: u16, active_epoch: u64) -> Vec<u8> {
        let mut data = vec![0u8; 72];
        data[TENURE_OFFSET..TENURE_OFFSET + 2].copy_from_slice(&tenure_epochs.to_le_bytes());
        data[ACTIVE_EPOCH_OFFSET..ACTIVE_EPOCH_OFFSET + 8]
            .copy_from_slice(&active_epoch.to_le_bytes());
        data
    }

    #[test]
    fn seat_active_when_tenure_nonzero() {
        let data = make_seat_data_ex(3, 7);
        assert!(is_seat_already_active(Some(&data)));
    }

    #[test]
    fn seat_not_active_when_tenure_zero() {
        let data = make_seat_data_ex(0, 0);
        assert!(!is_seat_already_active(Some(&data)));
    }

    #[test]
    fn seat_not_active_when_tenure_cleared_but_active_epoch_stale() {
        // Regression: BatchClearTenure zeros tenure_epochs but leaves
        // active_epoch at the old value. The old `active_epoch > 0` check
        // would incorrectly return true here.
        let data = make_seat_data_ex(0, 5);
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

    // --- other_device_keys_for_ip tests ---

    /// Build a minimal ClientSeat byte buffer with the given device key and
    /// tenure_epochs value.
    fn make_seat_with_device(device: &Pubkey, tenure_epochs: u16) -> Account {
        let mut data = vec![0u8; 72];
        data[8..40].copy_from_slice(device.as_ref());
        data[TENURE_OFFSET..TENURE_OFFSET + 2].copy_from_slice(&tenure_epochs.to_le_bytes());
        Account {
            data,
            ..Account::default()
        }
    }

    #[test]
    fn no_conflict_when_no_seats() {
        let target = Pubkey::new_unique();
        assert!(other_device_keys_for_ip(&[], &target).is_empty());
    }

    #[test]
    fn no_conflict_when_only_same_device() {
        let target = Pubkey::new_unique();
        let accounts = vec![(Pubkey::new_unique(), make_seat_with_device(&target, 1))];
        assert!(other_device_keys_for_ip(&accounts, &target).is_empty());
    }

    #[test]
    fn conflict_when_different_device() {
        let target = Pubkey::new_unique();
        let other = Pubkey::new_unique();
        let accounts = vec![(Pubkey::new_unique(), make_seat_with_device(&other, 1))];
        let result = other_device_keys_for_ip(&accounts, &target);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0], other);
    }

    #[test]
    fn conflict_filters_out_target_device() {
        let target = Pubkey::new_unique();
        let other = Pubkey::new_unique();
        let accounts = vec![
            (Pubkey::new_unique(), make_seat_with_device(&target, 1)),
            (Pubkey::new_unique(), make_seat_with_device(&other, 1)),
        ];
        let result = other_device_keys_for_ip(&accounts, &target);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0], other);
    }

    #[test]
    fn conflict_multiple_other_devices() {
        let target = Pubkey::new_unique();
        let other1 = Pubkey::new_unique();
        let other2 = Pubkey::new_unique();
        let accounts = vec![
            (Pubkey::new_unique(), make_seat_with_device(&other1, 1)),
            (Pubkey::new_unique(), make_seat_with_device(&other2, 1)),
        ];
        let result = other_device_keys_for_ip(&accounts, &target);
        assert_eq!(result.len(), 2);
    }

    #[test]
    fn no_conflict_when_other_device_seat_withdrawn() {
        let target = Pubkey::new_unique();
        let other = Pubkey::new_unique();
        let accounts = vec![(Pubkey::new_unique(), make_seat_with_device(&other, 0))];
        assert!(other_device_keys_for_ip(&accounts, &target).is_empty());
    }

    // --- user_owner_is_acceptable tests ---

    #[test]
    fn user_acceptable_when_owned_by_shred_oracle() {
        let oracle = Pubkey::new_unique();
        let wallet = Pubkey::new_unique();
        assert!(user_owner_is_acceptable(
            Some(oracle),
            Some(oracle),
            wallet,
            wallet,
            false,
        ));
    }

    #[test]
    fn user_acceptable_when_self_owned() {
        // New behavior: validator-owned (self-owned) Users are benign.
        let oracle = Pubkey::new_unique();
        let wallet = Pubkey::new_unique();
        assert!(user_owner_is_acceptable(
            Some(wallet),
            Some(oracle),
            wallet,
            wallet,
            false,
        ));
    }

    #[test]
    fn user_acceptable_when_owned_by_operator_key() {
        // Operator-key split: the oracle provisions the user owned by the
        // operator key, distinct from the wallet (withdraw authority). A
        // re-subscribe/top-up must treat that as self-owned.
        let oracle = Pubkey::new_unique();
        let wallet = Pubkey::new_unique();
        let operator = Pubkey::new_unique();
        assert!(user_owner_is_acceptable(
            Some(operator),
            Some(oracle),
            wallet,
            operator,
            false,
        ));
    }

    #[test]
    fn user_acceptable_self_owned_even_when_oracle_key_unknown() {
        // If we can't resolve the oracle pubkey, self-ownership still passes.
        let wallet = Pubkey::new_unique();
        assert!(user_owner_is_acceptable(
            Some(wallet),
            None,
            wallet,
            wallet,
            false,
        ));
    }

    #[test]
    fn user_rejected_when_owned_by_third_party() {
        let oracle = Pubkey::new_unique();
        let wallet = Pubkey::new_unique();
        let third_party = Pubkey::new_unique();
        assert!(!user_owner_is_acceptable(
            Some(third_party),
            Some(oracle),
            wallet,
            wallet,
            false,
        ));
    }

    #[test]
    fn user_rejected_when_owner_missing() {
        // Malformed account data → user_owner is None → bail (safe default).
        let oracle = Pubkey::new_unique();
        let wallet = Pubkey::new_unique();
        assert!(!user_owner_is_acceptable(
            None,
            Some(oracle),
            wallet,
            wallet,
            false,
        ));
    }

    #[test]
    fn user_rejected_when_third_party_and_oracle_unknown() {
        let wallet = Pubkey::new_unique();
        let third_party = Pubkey::new_unique();
        assert!(!user_owner_is_acceptable(
            Some(third_party),
            None,
            wallet,
            wallet,
            false,
        ));
    }

    #[test]
    fn test_user_acceptable_when_wallet_owns_escrow() {
        // Top-up of an operator-keyed seat: the user is owned by the operator
        // key (neither oracle nor wallet), but this wallet already owns the
        // seat's escrow, so the user was provisioned from it and is benign.
        let oracle = Pubkey::new_unique();
        let wallet = Pubkey::new_unique();
        let operator = Pubkey::new_unique();
        assert!(user_owner_is_acceptable(
            Some(operator),
            Some(oracle),
            wallet,
            wallet,
            true,
        ));
    }

    #[test]
    fn test_user_rejected_when_third_party_and_no_escrow() {
        // Same foreign owner, but without an escrow this wallet owns, the guard
        // must still reject — this is the collision case it exists to catch.
        let oracle = Pubkey::new_unique();
        let wallet = Pubkey::new_unique();
        let operator = Pubkey::new_unique();
        assert!(!user_owner_is_acceptable(
            Some(operator),
            Some(oracle),
            wallet,
            wallet,
            false,
        ));
    }
}
