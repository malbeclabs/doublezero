use std::{net::Ipv4Addr, ops::Range};

use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    DISCRIMINATOR_LEN, Discriminator, PrecomputedDiscriminator,
    types::{Flags, StorageGap},
};
use doublezero_revenue_distribution::types::{DoubleZeroEpoch, UnitShare16};
use solana_sdk::pubkey::Pubkey;
use svm_hash::sha2::Hash;

pub const PROGRAM_CONFIG_SEED_PREFIX: &[u8] = b"program_config";
pub const EXECUTION_CONTROLLER_SEED_PREFIX: &[u8] = b"execution_controller";
pub const DEVICE_HISTORY_SEED_PREFIX: &[u8] = b"device_history";
pub const CLIENT_SEAT_SEED_PREFIX: &[u8] = b"client_seat";
pub const METRO_HISTORY_SEED_PREFIX: &[u8] = b"metro_history";
pub const TOKEN_PDA_SEED_PREFIX: &[u8] = b"token";
pub const PAYMENT_ESCROW_SEED_PREFIX: &[u8] = b"payment_escrow";
pub const VALIDATOR_CLIENT_REWARDS_SEED_PREFIX: &[u8] = b"validator_client_rewards";
pub const VALIDATOR_PUBLISHER_REWARDS_SEED_PREFIX: &[u8] = b"validator_publisher_rewards";
pub const SHRED_REWARD_TOKEN_SEED_PREFIX: &[u8] = b"shred_reward_token";
pub const INSTANT_ALLOCATION_REQUEST_SEED_PREFIX: &[u8] = b"instant_seat_allocation_request";
pub const WITHDRAW_SEAT_REQUEST_SEED_PREFIX: &[u8] = b"withdraw_seat_request";
pub const SHRED_DISTRIBUTION_SEED_PREFIX: &[u8] = b"shred_distribution";
pub const SHRED_DISTRIBUTION_JOURNAL_SEED_PREFIX: &[u8] = b"shred_distribution_journal";
pub const CLAIM_HOLDING_SEED_PREFIX: &[u8] = b"claim";

pub fn find_program_config_address() -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[PROGRAM_CONFIG_SEED_PREFIX],
        &crate::shred_subscription::ID,
    )
}

pub fn find_execution_controller_address() -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[EXECUTION_CONTROLLER_SEED_PREFIX],
        &crate::shred_subscription::ID,
    )
}

pub fn find_device_history_address(device_key: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[DEVICE_HISTORY_SEED_PREFIX, device_key.as_ref()],
        &crate::shred_subscription::ID,
    )
}

pub fn find_client_seat_address(device_key: &Pubkey, client_ip_bits: u32) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            CLIENT_SEAT_SEED_PREFIX,
            device_key.as_ref(),
            &client_ip_bits.to_le_bytes(),
        ],
        &crate::shred_subscription::ID,
    )
}

pub fn find_metro_history_address(exchange_key: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[METRO_HISTORY_SEED_PREFIX, exchange_key.as_ref()],
        &crate::shred_subscription::ID,
    )
}

pub fn find_token_pda_address(token_owner_key: &Pubkey, mint_key: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            TOKEN_PDA_SEED_PREFIX,
            token_owner_key.as_ref(),
            mint_key.as_ref(),
        ],
        &crate::shred_subscription::ID,
    )
}

pub fn find_validator_client_rewards_address(client_id: u16) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            VALIDATOR_CLIENT_REWARDS_SEED_PREFIX,
            &client_id.to_le_bytes(),
        ],
        &crate::shred_subscription::ID,
    )
}

pub fn find_validator_publisher_rewards_address(node_id: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[VALIDATOR_PUBLISHER_REWARDS_SEED_PREFIX, node_id.as_ref()],
        &crate::shred_subscription::ID,
    )
}

pub fn find_shred_reward_token_address(mint_key: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[SHRED_REWARD_TOKEN_SEED_PREFIX, mint_key.as_ref()],
        &crate::shred_subscription::ID,
    )
}

pub fn find_instant_allocation_request_address(
    device_key: &Pubkey,
    client_ip_bits: u32,
) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            INSTANT_ALLOCATION_REQUEST_SEED_PREFIX,
            device_key.as_ref(),
            &client_ip_bits.to_le_bytes(),
        ],
        &crate::shred_subscription::ID,
    )
}

pub fn find_withdraw_seat_request_address(client_seat_key: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[WITHDRAW_SEAT_REQUEST_SEED_PREFIX, client_seat_key.as_ref()],
        &crate::shred_subscription::ID,
    )
}

pub fn find_payment_escrow_address(
    client_seat_key: &Pubkey,
    withdraw_authority_key: &Pubkey,
) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            PAYMENT_ESCROW_SEED_PREFIX,
            client_seat_key.as_ref(),
            withdraw_authority_key.as_ref(),
        ],
        &crate::shred_subscription::ID,
    )
}

pub fn find_shred_distribution_address(subscription_epoch: u64) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            SHRED_DISTRIBUTION_SEED_PREFIX,
            &subscription_epoch.to_le_bytes(),
        ],
        &crate::shred_subscription::ID,
    )
}

pub fn find_shred_distribution_journal_address(
    subscription_epoch: u64,
    mint_key: &Pubkey,
) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            SHRED_DISTRIBUTION_JOURNAL_SEED_PREFIX,
            &subscription_epoch.to_le_bytes(),
            mint_key.as_ref(),
        ],
        &crate::shred_subscription::ID,
    )
}

pub fn find_claim_holding_address(
    parent_pda_key: &Pubkey,
    subscription_epoch: u64,
    mint_key: &Pubkey,
) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            CLAIM_HOLDING_SEED_PREFIX,
            parent_pda_key.as_ref(),
            &subscription_epoch.to_le_bytes(),
            mint_key.as_ref(),
        ],
        &crate::shred_subscription::ID,
    )
}

// ---------------------------------------------------------------------------
// ProgramConfig raw-byte parsing.
//
// Layout (ZeroCopy with 8-byte discriminator prefix):
//   [0..8)   discriminator
//   [8..16)  flags: Flags (u64, LE) -- first field, stable since account
//            was introduced. Bit 2 gates prorated instant service.
//   ...      (remaining fields irrelevant for the CLI today)
// ---------------------------------------------------------------------------

pub const PROGRAM_CONFIG_DISCRIMINATOR: Discriminator<DISCRIMINATOR_LEN> =
    Discriminator::new_sha2(b"dz::account::program_config");

pub const PROGRAM_CONFIG_FLAGS_OFFSET: usize = DISCRIMINATOR_LEN;
pub const PROGRAM_CONFIG_SHRED_ORACLE_KEY_OFFSET: usize = DISCRIMINATOR_LEN + 48;

const PROGRAM_CONFIG_FLAG_IS_PRORATED_SERVICE_ENABLED_BIT: u64 = 1 << 2;
const PROGRAM_CONFIG_FLAG_DISTRIBUTE_VALIDATOR_REWARDS_ENABLED_BIT: u64 = 1 << 5;

/// Returns `true` if the `is_prorated_service_enabled` bit is set on the
/// raw `ProgramConfig` account data. Returns `false` for accounts that are
/// too short to contain the flags word (e.g. a pre-prorated program
/// deployment), which is the same behavior as the flag being unset.
pub fn is_prorated_service_enabled(data: &[u8]) -> bool {
    if data.len() < PROGRAM_CONFIG_FLAGS_OFFSET + 8 {
        return false;
    }
    let Ok(flags_bytes) =
        <[u8; 8]>::try_from(&data[PROGRAM_CONFIG_FLAGS_OFFSET..PROGRAM_CONFIG_FLAGS_OFFSET + 8])
    else {
        return false;
    };
    let flags = u64::from_le_bytes(flags_bytes);
    flags & PROGRAM_CONFIG_FLAG_IS_PRORATED_SERVICE_ENABLED_BIT != 0
}

/// Offset of `ExecutionController::last_settled_epoch` within the raw account
/// data (after the discriminator). On-chain field layout preceding it:
///
/// ```text
/// phase_field(u8) + bump_seed(u8) + pad(2) + total_metros(u16) +
/// total_enabled_devices(u16) + total_client_seats(u32) +
/// oracle_instant_request_count(u16) + validator_client_ids_count(u8) + pad(1)
/// + flags(8) = 24 bytes to current_subscription_epoch, then 120 more:
/// current_subscription_epoch(u64) +
/// updated_device_prices_count(u16) + settled_devices_count(u16) +
/// settled_client_seats_count(u16) + total_devices(u16) + last_settled_slot(u64)
/// + last_updating_prices_slot(u64) + last_open_for_requests_slot(u64) +
/// last_closed_for_requests_slot(u64) + epoch_round_commitment(32) +
/// epoch_round_reveal(32) + next_seat_funding_index(u64) = 120 → 24 + 120 = 144.
/// ```
pub const EXECUTION_CONTROLLER_LAST_SETTLED_EPOCH_OFFSET: usize = DISCRIMINATOR_LEN + 144;

/// Reads `last_settled_epoch` from raw `ExecutionController` account data.
/// Returns `None` if the account is too short to contain the field.
pub fn parse_execution_controller_last_settled_epoch(data: &[u8]) -> Option<u64> {
    let start = EXECUTION_CONTROLLER_LAST_SETTLED_EPOCH_OFFSET;
    let bytes = data.get(start..start + 8)?;
    Some(u64::from_le_bytes(<[u8; 8]>::try_from(bytes).ok()?))
}

/// Returns `true` if the `distribute_validator_rewards_enabled` bit is set
/// on the raw `ProgramConfig` account data. Mirrors the on-chain
/// `ProgramConfig::FLAG_DISTRIBUTE_VALIDATOR_REWARDS_ENABLED_BIT` (bit 5).
/// Returns `false` when the account is too short or the flag is unset —
/// the on-chain `DistributeValidatorRewards` handler rejects with
/// "Distribute validator rewards is disabled" in that state, so callers
/// should skip distribute attempts when this returns `false`.
pub fn is_distribute_validator_rewards_enabled(data: &[u8]) -> bool {
    if data.len() < PROGRAM_CONFIG_FLAGS_OFFSET + 8 {
        return false;
    }
    let Ok(flags_bytes) =
        <[u8; 8]>::try_from(&data[PROGRAM_CONFIG_FLAGS_OFFSET..PROGRAM_CONFIG_FLAGS_OFFSET + 8])
    else {
        return false;
    };
    let flags = u64::from_le_bytes(flags_bytes);
    flags & PROGRAM_CONFIG_FLAG_DISTRIBUTE_VALIDATOR_REWARDS_ENABLED_BIT != 0
}

/// Parse the `shred_oracle_key` from a `ProgramConfig` account. Returns
/// `None` when the data is too short or the discriminator does not match.
pub fn parse_program_config_shred_oracle_key(data: &[u8]) -> Option<Pubkey> {
    if data.len() < PROGRAM_CONFIG_SHRED_ORACLE_KEY_OFFSET + 32 {
        return None;
    }
    let expected_disc =
        borsh::to_vec(&PROGRAM_CONFIG_DISCRIMINATOR).expect("discriminator serialization");
    if data[..DISCRIMINATOR_LEN] != expected_disc[..] {
        return None;
    }
    Some(Pubkey::new_from_array(
        data[PROGRAM_CONFIG_SHRED_ORACLE_KEY_OFFSET..PROGRAM_CONFIG_SHRED_ORACLE_KEY_OFFSET + 32]
            .try_into()
            .ok()?,
    ))
}

// ---------------------------------------------------------------------------
// ClientSeat raw-byte parsing (for the `list` command).
//
// Layout (ZeroCopy with 8-byte discriminator prefix):
//   [0..8)    discriminator
//   [8..40)   device_key: Pubkey
//   [40..44)  client_ip_bits: u32
//   [44..46)  _padding: [u8; 2]
//   [46..48)  tenure_epochs: u16
//   [48..56)  _flags: Flags (u64)
//   [56..64)  funded_epoch: u64
//   [64..72)  active_epoch: u64
//   [72..80)  funding_index: u64
//   [80..112) new_settlement_sort_key: Hash
//   [112..144) funding_authority_key: Pubkey
//   [144..148) escrow_count: u32
//   [148..150) override_usdc_price_dollars: u16
//   [152..160) subscription_start_slot: u64
//   [160..162) last_usdc_price_dollars: u16
// ---------------------------------------------------------------------------

pub const CLIENT_SEAT_DISCRIMINATOR: Discriminator<DISCRIMINATOR_LEN> =
    Discriminator::new_sha2(b"dz::account::client_seat");

pub const CLIENT_SEAT_DEVICE_KEY_OFFSET: usize = DISCRIMINATOR_LEN;
pub const CLIENT_SEAT_CLIENT_IP_OFFSET: usize = DISCRIMINATOR_LEN + 32;
pub const CLIENT_SEAT_TENURE_OFFSET: usize = DISCRIMINATOR_LEN + 38;
pub const CLIENT_SEAT_FUNDED_EPOCH_OFFSET: usize = DISCRIMINATOR_LEN + 48;
pub const CLIENT_SEAT_ACTIVE_EPOCH_OFFSET: usize = DISCRIMINATOR_LEN + 56;
pub const CLIENT_SEAT_FLAGS_OFFSET: usize = DISCRIMINATOR_LEN + 40;
pub const CLIENT_SEAT_FUNDING_INDEX_OFFSET: usize = DISCRIMINATOR_LEN + 64;
pub const CLIENT_SEAT_OVERRIDE_USDC_PRICE_OFFSET: usize = DISCRIMINATOR_LEN + 140;
pub const CLIENT_SEAT_LAST_USDC_PRICE_OFFSET: usize = DISCRIMINATOR_LEN + 152;

/// Parse a `ClientSeat` from raw account data. Returns
/// `(device_key, client_ip, tenure_epochs, funded_epoch, active_epoch)`.
pub fn parse_client_seat(data: &[u8]) -> Option<(Pubkey, Ipv4Addr, u16, u64, u64)> {
    if data.len() < CLIENT_SEAT_ACTIVE_EPOCH_OFFSET + 8 {
        return None;
    }
    let device_key = Pubkey::new_from_array(
        data[CLIENT_SEAT_DEVICE_KEY_OFFSET..CLIENT_SEAT_DEVICE_KEY_OFFSET + 32]
            .try_into()
            .ok()?,
    );
    let client_ip_bits = u32::from_le_bytes(
        data[CLIENT_SEAT_CLIENT_IP_OFFSET..CLIENT_SEAT_CLIENT_IP_OFFSET + 4]
            .try_into()
            .ok()?,
    );
    let tenure_epochs = u16::from_le_bytes(
        data[CLIENT_SEAT_TENURE_OFFSET..CLIENT_SEAT_TENURE_OFFSET + 2]
            .try_into()
            .ok()?,
    );
    let funded_epoch = u64::from_le_bytes(
        data[CLIENT_SEAT_FUNDED_EPOCH_OFFSET..CLIENT_SEAT_FUNDED_EPOCH_OFFSET + 8]
            .try_into()
            .ok()?,
    );
    let active_epoch = u64::from_le_bytes(
        data[CLIENT_SEAT_ACTIVE_EPOCH_OFFSET..CLIENT_SEAT_ACTIVE_EPOCH_OFFSET + 8]
            .try_into()
            .ok()?,
    );
    Some((
        device_key,
        Ipv4Addr::from(client_ip_bits),
        tenure_epochs,
        funded_epoch,
        active_epoch,
    ))
}

/// Returns the seat's `last_usdc_price_dollars` (whole USDC dollars charged
/// at the most recent allocation). Returns `None` when the account data is
/// too short (e.g. a program predating prorated-service fields). A returned
/// value of `Some(0)` means the field exists but is zero — a "pre-upgrade"
/// seat that has not yet been repopulated by a settlement cycle.
pub fn parse_client_seat_last_usdc_price_dollars(data: &[u8]) -> Option<u16> {
    if data.len() < CLIENT_SEAT_LAST_USDC_PRICE_OFFSET + 2 {
        return None;
    }
    let bytes = <[u8; 2]>::try_from(
        &data[CLIENT_SEAT_LAST_USDC_PRICE_OFFSET..CLIENT_SEAT_LAST_USDC_PRICE_OFFSET + 2],
    )
    .ok()?;
    Some(u16::from_le_bytes(bytes))
}

const CLIENT_SEAT_FLAG_HAS_PRICE_OVERRIDE_BIT: u64 = 1 << 0;

/// If the `ClientSeat` has a price override, returns the override amount in
/// micro-USDC. Otherwise returns `None`.
pub fn parse_client_seat_price_override(data: &[u8]) -> Option<u64> {
    if data.len() < CLIENT_SEAT_OVERRIDE_USDC_PRICE_OFFSET + 2 {
        return None;
    }
    let flags = u64::from_le_bytes(
        data[CLIENT_SEAT_FLAGS_OFFSET..CLIENT_SEAT_FLAGS_OFFSET + 8]
            .try_into()
            .ok()?,
    );
    if flags & CLIENT_SEAT_FLAG_HAS_PRICE_OVERRIDE_BIT == 0 {
        return None;
    }
    let override_dollars = u16::from_le_bytes(
        data[CLIENT_SEAT_OVERRIDE_USDC_PRICE_OFFSET..CLIENT_SEAT_OVERRIDE_USDC_PRICE_OFFSET + 2]
            .try_into()
            .ok()?,
    );
    Some(override_dollars as u64 * 1_000_000)
}

// ---------------------------------------------------------------------------
// PaymentEscrow raw-byte parsing.
//
// Layout (ZeroCopy with 8-byte discriminator prefix):
//   [0..8)   discriminator
//   [8..40)  client_seat_key: Pubkey
//   [40..72) withdraw_authority_key: Pubkey
//   [72..80) usdc_balance: u64
// ---------------------------------------------------------------------------

pub const PAYMENT_ESCROW_DISCRIMINATOR: Discriminator<DISCRIMINATOR_LEN> =
    Discriminator::new_sha2(b"dz::account::payment_escrow");

pub const PAYMENT_ESCROW_SEAT_OFFSET: usize = DISCRIMINATOR_LEN;
pub const PAYMENT_ESCROW_AUTHORITY_OFFSET: usize = DISCRIMINATOR_LEN + 32;
pub const PAYMENT_ESCROW_BALANCE_OFFSET: usize = DISCRIMINATOR_LEN + 64;

/// Parse a `PaymentEscrow` from raw account data. Returns
/// `(client_seat_key, withdraw_authority_key, usdc_balance)`.
pub fn parse_payment_escrow(data: &[u8]) -> Option<(Pubkey, Pubkey, u64)> {
    if data.len() < PAYMENT_ESCROW_BALANCE_OFFSET + 8 {
        return None;
    }
    let client_seat_key = Pubkey::new_from_array(
        data[PAYMENT_ESCROW_SEAT_OFFSET..PAYMENT_ESCROW_SEAT_OFFSET + 32]
            .try_into()
            .ok()?,
    );
    let withdraw_authority_key = Pubkey::new_from_array(
        data[PAYMENT_ESCROW_AUTHORITY_OFFSET..PAYMENT_ESCROW_AUTHORITY_OFFSET + 32]
            .try_into()
            .ok()?,
    );
    let usdc_balance = u64::from_le_bytes(
        data[PAYMENT_ESCROW_BALANCE_OFFSET..PAYMENT_ESCROW_BALANCE_OFFSET + 8]
            .try_into()
            .ok()?,
    );
    Some((client_seat_key, withdraw_authority_key, usdc_balance))
}

// ---------------------------------------------------------------------------
// Ring buffer epoch lookup, shared by DeviceHistory and MetroHistory.
//
// Both accounts embed a `RingBuffer<_, 32>` laid out as:
//   [ring_offset]                     current_index: u8
//   [ring_offset + 1]                 total_count: u8
//   [ring_offset + 2..ring_offset + 8) padding
//   [ring_offset + 8..)               entries, each starting with epoch: u64
// ---------------------------------------------------------------------------

const RING_BUFFER_CAPACITY: usize = 32;

/// Returns the byte offset of the entry holding `epoch`, or `None` when no
/// entry matches. Mirrors the onchain `RingBuffer::find`: walk backwards from
/// `current_index`, bounded by `total_count`. The bound matters — scanning all
/// 32 slots would make `epoch == 0` match a zero-initialized slot.
fn find_ring_buffer_entry_offset(
    data: &[u8],
    ring_offset: usize,
    entry_size: usize,
    epoch: u64,
) -> Option<usize> {
    let current_index = *data.get(ring_offset)? as usize;
    let total_count = (*data.get(ring_offset + 1)? as usize).min(RING_BUFFER_CAPACITY);
    let entries_offset = ring_offset + 8;

    (0..total_count).find_map(|steps_back| {
        let index = (current_index + RING_BUFFER_CAPACITY - steps_back) % RING_BUFFER_CAPACITY;
        let entry_offset = entries_offset + index * entry_size;
        let entry_epoch = u64::from_le_bytes(
            <[u8; 8]>::try_from(data.get(entry_offset..entry_offset + 8)?).ok()?,
        );
        (entry_epoch == epoch).then_some(entry_offset)
    })
}

/// Combines a metro base price with a device's signed premium, mirroring the
/// onchain `DeviceSubscription::usdc_price_dollars`.
pub fn seat_usdc_price_dollars(metro_price_dollars: u16, device_premium_dollars: i16) -> u16 {
    if device_premium_dollars < 0 {
        metro_price_dollars.saturating_sub(device_premium_dollars.unsigned_abs())
    } else {
        metro_price_dollars.saturating_add(device_premium_dollars.unsigned_abs())
    }
}

// ---------------------------------------------------------------------------
// DeviceHistory raw-byte parsing.
//
// Layout (ZeroCopy with 8-byte discriminator prefix):
//   [0..8)    discriminator
//   [8..40)   device_key: Pubkey
//   [40..48)  flags: u64
//   [48)      bump_seed: u8
//   [49)      usdc_token_pda_bump_seed: u8
//   [50..56)  _padding: [u8; 6]
//   [56..88)  metro_exchange_key: Pubkey
//   [88..90)  active_granted_seats: u16
//   [90..92)  active_total_available_seats: u16
//   [92..120) _padding
//   [120..216) StorageGap<3>
//   [216..)   subscriptions: RingBuffer<DeviceSubscription, 32>
// ---------------------------------------------------------------------------

pub const DEVICE_HISTORY_DISCRIMINATOR: Discriminator<DISCRIMINATOR_LEN> =
    Discriminator::new_sha2(b"dz::account::device_history");

pub const DEVICE_HISTORY_DEVICE_KEY_OFFSET: usize = DISCRIMINATOR_LEN;
pub const DEVICE_HISTORY_FLAGS_OFFSET: usize = DISCRIMINATOR_LEN + 32;
pub const DEVICE_HISTORY_EXCHANGE_KEY_OFFSET: usize = DISCRIMINATOR_LEN + 32 + 16;
const DEVICE_HISTORY_ACTIVE_GRANTED_SEATS_OFFSET: usize = DISCRIMINATOR_LEN + 80;
const DEVICE_HISTORY_ACTIVE_TOTAL_AVAILABLE_SEATS_OFFSET: usize = DISCRIMINATOR_LEN + 82;
const DEVICE_HISTORY_RING_OFFSET: usize = DISCRIMINATOR_LEN + 208; // after active seat fields + StorageGap<3> (128 bytes total)
const DEVICE_HISTORY_ENTRY_SIZE: usize = 80; // EpochEntry<DeviceSubscription>

/// Parse the metro exchange pubkey directly from raw `DeviceHistory` account data.
pub fn parse_exchange_key_from_device_history(data: &[u8]) -> Option<Pubkey> {
    let start = DEVICE_HISTORY_EXCHANGE_KEY_OFFSET;
    let end = start + 32;
    if data.len() < end {
        return None;
    }
    Some(Pubkey::new_from_array(data[start..end].try_into().ok()?))
}

pub struct DeviceHistoryInfo {
    pub device_key: Pubkey,
    pub exchange_key: Pubkey,
    pub is_enabled: bool,
    pub current_epoch: u64,
    pub current_premium: i16,
    pub requested_seat_count: u16,
    pub total_available_seats: u16,
    pub granted_seat_count: u16,
}

/// Parse a `DeviceHistory` account's current-epoch pricing from raw bytes.
pub fn parse_device_history(data: &[u8]) -> Option<DeviceHistoryInfo> {
    let ring_offset = DEVICE_HISTORY_RING_OFFSET;
    if data.len() < ring_offset + 8 {
        return None;
    }

    let device_key = Pubkey::new_from_array(
        data[DEVICE_HISTORY_DEVICE_KEY_OFFSET..DEVICE_HISTORY_DEVICE_KEY_OFFSET + 32]
            .try_into()
            .ok()?,
    );
    let flags = u64::from_le_bytes(
        data[DEVICE_HISTORY_FLAGS_OFFSET..DEVICE_HISTORY_FLAGS_OFFSET + 8]
            .try_into()
            .ok()?,
    );
    let is_enabled = flags & (1 << 1) != 0;
    let exchange_key = Pubkey::new_from_array(
        data[DEVICE_HISTORY_EXCHANGE_KEY_OFFSET..DEVICE_HISTORY_EXCHANGE_KEY_OFFSET + 32]
            .try_into()
            .ok()?,
    );

    let current_index = data[ring_offset] as usize;
    let total_count = data[ring_offset + 1];
    if total_count == 0 {
        return None;
    }

    let entries_offset = ring_offset + 8; // skip current_index + total_count + padding
    let entry_offset = entries_offset + current_index * DEVICE_HISTORY_ENTRY_SIZE;
    if data.len() < entry_offset + 16 {
        return None;
    }

    let current_epoch = u64::from_le_bytes(data[entry_offset..entry_offset + 8].try_into().ok()?);
    let current_premium =
        i16::from_le_bytes(data[entry_offset + 8..entry_offset + 10].try_into().ok()?);
    let requested_seat_count =
        u16::from_le_bytes(data[entry_offset + 10..entry_offset + 12].try_into().ok()?);

    // Read device-level active seat fields from the header (outside the ring
    // buffer). These are maintained by instant allocation/withdrawal and synced
    // during settlement, so they always reflect the current state.
    let total_available_seats = u16::from_le_bytes(
        data[DEVICE_HISTORY_ACTIVE_TOTAL_AVAILABLE_SEATS_OFFSET
            ..DEVICE_HISTORY_ACTIVE_TOTAL_AVAILABLE_SEATS_OFFSET + 2]
            .try_into()
            .ok()?,
    );
    let granted_seat_count = u16::from_le_bytes(
        data[DEVICE_HISTORY_ACTIVE_GRANTED_SEATS_OFFSET
            ..DEVICE_HISTORY_ACTIVE_GRANTED_SEATS_OFFSET + 2]
            .try_into()
            .ok()?,
    );

    Some(DeviceHistoryInfo {
        device_key,
        exchange_key,
        is_enabled,
        current_epoch,
        current_premium,
        requested_seat_count,
        total_available_seats,
        granted_seat_count,
    })
}

/// Returns the device's `usdc_metro_premium_dollars` for `epoch`, or `None`
/// when the ring buffer holds no entry for that epoch. Callers that price an
/// instant seat allocation want this rather than `parse_device_history`: the
/// program charges from the entry at `last_settled_epoch`, which during
/// `OpenForRequests` is one epoch behind the newest entry.
pub fn parse_device_history_premium_at_epoch(data: &[u8], epoch: u64) -> Option<i16> {
    let entry_offset = find_ring_buffer_entry_offset(
        data,
        DEVICE_HISTORY_RING_OFFSET,
        DEVICE_HISTORY_ENTRY_SIZE,
        epoch,
    )?;
    let bytes = <[u8; 2]>::try_from(data.get(entry_offset + 8..entry_offset + 10)?).ok()?;
    Some(i16::from_le_bytes(bytes))
}

// ---------------------------------------------------------------------------
// MetroHistory raw-byte parsing.
//
// Layout (ZeroCopy with 8-byte discriminator prefix):
//   [0..8)     discriminator
//   [8..40)    exchange_key: Pubkey
//   [40..48)   _flags: Flags (u64)
//   [48..50)   total_initialized_devices: u16
//   [50..56)   _padding: [u8; 6]
//   [56..184)  StorageGap<4> ([[u8; 32]; 4] = 128 bytes)
//   [184)      ring_buffer.current_index: u8
//   [185)      ring_buffer.total_count: u8
//   [186..192) padding
//   [192..)    entries: 32 × EpochEntry<MetroPrice> (80 bytes each)
// ---------------------------------------------------------------------------

pub const METRO_HISTORY_DISCRIMINATOR: Discriminator<DISCRIMINATOR_LEN> =
    Discriminator::new_sha2(b"dz::account::metro_history");

pub const METRO_HISTORY_EXCHANGE_KEY_OFFSET: usize = DISCRIMINATOR_LEN;
const METRO_HISTORY_DEVICES_OFFSET: usize = DISCRIMINATOR_LEN + 40;
const METRO_HISTORY_RING_OFFSET: usize = DISCRIMINATOR_LEN + 176; // after StorageGap<4> (128 bytes)
const METRO_HISTORY_ENTRY_SIZE: usize = 80; // EpochEntry<MetroPrice>

pub struct MetroHistoryInfo {
    pub exchange_key: Pubkey,
    pub total_devices: u16,
    pub current_epoch: u64,
    pub current_usdc_price: u16,
}

/// Parse a `MetroHistory` account's current-epoch pricing from raw bytes.
pub fn parse_metro_history(data: &[u8]) -> Option<MetroHistoryInfo> {
    let ring_offset = METRO_HISTORY_RING_OFFSET;
    if data.len() < ring_offset + 8 {
        return None;
    }

    let exchange_key = Pubkey::new_from_array(
        data[METRO_HISTORY_EXCHANGE_KEY_OFFSET..METRO_HISTORY_EXCHANGE_KEY_OFFSET + 32]
            .try_into()
            .ok()?,
    );
    let total_devices = u16::from_le_bytes(
        data[METRO_HISTORY_DEVICES_OFFSET..METRO_HISTORY_DEVICES_OFFSET + 2]
            .try_into()
            .ok()?,
    );

    let current_index = data[ring_offset] as usize;
    let total_count = data[ring_offset + 1];
    if total_count == 0 {
        return None;
    }

    let entries_offset = ring_offset + 8; // skip current_index + total_count + padding
    let entry_offset = entries_offset + current_index * METRO_HISTORY_ENTRY_SIZE;
    if data.len() < entry_offset + 10 {
        return None;
    }

    let current_epoch = u64::from_le_bytes(data[entry_offset..entry_offset + 8].try_into().ok()?);
    let current_usdc_price =
        u16::from_le_bytes(data[entry_offset + 8..entry_offset + 10].try_into().ok()?);

    Some(MetroHistoryInfo {
        exchange_key,
        total_devices,
        current_epoch,
        current_usdc_price,
    })
}

/// Returns the metro's `usdc_price_dollars` for `epoch`, or `None` when the
/// ring buffer holds no entry for that epoch. See
/// [`parse_device_history_premium_at_epoch`] for why instant-allocation
/// pricing needs a specific epoch rather than the newest entry.
pub fn parse_metro_history_price_at_epoch(data: &[u8], epoch: u64) -> Option<u16> {
    let entry_offset = find_ring_buffer_entry_offset(
        data,
        METRO_HISTORY_RING_OFFSET,
        METRO_HISTORY_ENTRY_SIZE,
        epoch,
    )?;
    let bytes = <[u8; 2]>::try_from(data.get(entry_offset + 8..entry_offset + 10)?).ok()?;
    Some(u16::from_le_bytes(bytes))
}

// ---------------------------------------------------------------------------
// ValidatorClientRewards, ShredRewardToken and ValidatorPublisherRewards:
// layout mirrored from the onchain `doublezero-shred-subscription` program
// (state module). Kept here to avoid pulling the program crate as a dependency
// just for three account types. If the onchain layout changes, update both
// this file and the discriminator strings together.
// ---------------------------------------------------------------------------

#[derive(Clone, Copy, Debug, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ValidatorClientRewards {
    pub client_id: u16,
    pub bump_seed: u8,
    _padding_0: [u8; 5],
    pub manager_key: Pubkey,
    pub short_description_bytes: [u8; 64],
    pub claim_holding_count: u32,
    _padding_1: [u8; 4],
    _gap: StorageGap<2>,
}

impl PrecomputedDiscriminator for ValidatorClientRewards {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::validator_client_rewards");
}

// `[u8; 64]` is wider than the array sizes `std` implements `Default` for, so
// this cannot be derived. The onchain struct carries the same manual impl.
impl Default for ValidatorClientRewards {
    fn default() -> Self {
        Zeroable::zeroed()
    }
}

impl ValidatorClientRewards {
    pub fn checked_short_description(&self) -> Option<&str> {
        let end = self.short_description_bytes.iter().rposition(|&b| b != 0)?;
        std::str::from_utf8(&self.short_description_bytes[..=end]).ok()
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ShredRewardToken {
    pub mint_key: Pubkey,
    pub flags: Flags,
    pub max_slippage_bps: UnitShare16,
    _padding_0: [u8; 6],
    _gap: StorageGap<2>,
}

impl PrecomputedDiscriminator for ShredRewardToken {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::shred_reward_token");
}

impl ShredRewardToken {
    pub const FLAG_IS_ENABLED_BIT: usize = 1;

    pub fn is_enabled(&self) -> bool {
        self.flags.bit(Self::FLAG_IS_ENABLED_BIT)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ValidatorPublisherRewards {
    pub node_id: Pubkey,
    pub rewards_token_owner_key: Pubkey,
    pub rewards_token_mint_key: Pubkey,
    _gap: StorageGap<4>,
}

impl PrecomputedDiscriminator for ValidatorPublisherRewards {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::validator_publisher_rewards");
}

// ---------------------------------------------------------------------------
// ShredDistribution + ShredDistributionJournal + the
// ValidatorClientRewardsConfig field they nest. Layouts mirrored from
// `malbeclabs/doublezero-shreds` (program crate). Vendored here so the
// offchain CLI can `bytemuck::from_bytes` these accounts without depending
// on the shreds program crate. Remove once the shreds repo is merged into
// the monorepo.
//
// The compile-time `const _: () = assert!(...)` lines at the bottom of this
// block mirror the on-chain `assert!(zero_copy::data_end::<T>() == N)` for
// each Pod struct (`data_end::<T>() == DISCRIMINATOR_LEN + size_of::<T>()`).
// Without them, a silent upstream drift in any field — or in
// `Flags`/`StorageGap<N>`'s size against a different `program-tools` pin —
// would shift `remaining_data`'s start by some number of bytes. Bitmap
// reads via `publisher_accumulation_bitmap_{start,end}_index` would then
// land on the wrong bytes and `bitmap_bit_set` would return garbage,
// silently undercounting or duplicating distribute work. If on-chain
// changes any of these layouts, update both sides together (offchain
// struct + the expected size below + the on-chain `assert!`).
// ---------------------------------------------------------------------------

pub const MAX_VALIDATOR_CLIENT_REWARDS_PROPORTIONS: usize = 32;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(4))]
pub struct ValidatorClientRewardsProportion {
    pub id: u16,
    pub rewards_proportion: UnitShare16,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(4))]
pub struct ValidatorClientRewardProportions {
    pub set_bitmap: u32,
    pub proportions: [ValidatorClientRewardsProportion; MAX_VALIDATOR_CLIENT_REWARDS_PROPORTIONS],
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ValidatorClientRewardsConfig {
    pub default_proportion: UnitShare16,
    _padding_0: [u8; 2],
    pub proportions: ValidatorClientRewardProportions,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ShredDistribution {
    pub subscription_epoch: u64,
    pub flags: Flags,
    pub associated_dz_epoch: DoubleZeroEpoch,
    pub bump_seed: u8,
    pub ata_usdc_bump_seed: u8,
    pub ata_2z_bump_seed: u8,
    _padding_0: [u8; 1],
    pub device_count: u16,
    pub client_seat_count: u16,
    pub journal_count: u16,
    pub validator_rewards_proportion: UnitShare16,
    pub total_publishing_validators: u32,
    pub validator_rewards_merkle_root: Hash,
    pub collected_usdc_payments: u64,
    pub contributor_collected_2z_converted_from_usdc: u64,
    pub contributor_usdc_swapped: u64,
    pub validator_client_rewards_config: ValidatorClientRewardsConfig,
    pub accumulated_validator_rewards_count: u32,
    _padding_1: [u8; 28],
    pub total_published_leader_slots: u32,
    _padding_2: [u8; 28],
    _gap: StorageGap<3>,
}

impl PrecomputedDiscriminator for ShredDistribution {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::shred_distribution");
}

impl ShredDistribution {
    pub const FLAG_VALIDATOR_REWARDS_CALCULATION_FINALIZED_BIT: usize = 1;
    pub const FLAG_VALIDATOR_REWARDS_ACCUMULATED_BIT: usize = 2;
    pub const FLAG_INTEGRATION_FUNDED_BIT: usize = 3;

    #[inline]
    pub fn is_validator_rewards_calculation_finalized(&self) -> bool {
        self.flags
            .bit(Self::FLAG_VALIDATOR_REWARDS_CALCULATION_FINALIZED_BIT)
    }

    #[inline]
    pub fn is_validator_rewards_accumulated(&self) -> bool {
        self.flags.bit(Self::FLAG_VALIDATOR_REWARDS_ACCUMULATED_BIT)
    }

    #[inline]
    pub fn is_integration_funded(&self) -> bool {
        self.flags.bit(Self::FLAG_INTEGRATION_FUNDED_BIT)
    }
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct ShredDistributionJournal {
    pub subscription_epoch: u64,
    pub mint_key: Pubkey,
    pub reward_mint_key: Pubkey,
    flags: Flags,
    pub usdc_swapped_amount: u64,
    pub tokens_received_amount: u64,
    pub publisher_accumulation_bitmap_start_index: u32,
    pub publisher_accumulation_bitmap_end_index: u32,
    pub client_accumulation_bitmap_start_index: u32,
    pub client_accumulation_bitmap_end_index: u32,
    pub validator_pool: u64,
    pub total_leader_slots: u32,
    _padding_0: [u8; 4],
    pub accumulated_publisher_slots_scaled: u64,
    pub accumulated_client_slots_scaled: u64,
    pub accumulated_publisher_leaf_count: u32,
    pub distributed_publisher_leaf_count: u32,
    pub distributed_amount: u64,
    pub accumulated_client_leaf_count: u32,
    pub distributed_client_leaf_count: u32,
    _padding_1: [u8; 16],
    pub first_distribute_timestamp: i64,
    _gap: StorageGap<3>,
}

impl PrecomputedDiscriminator for ShredDistributionJournal {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::shred_distribution_journal");
}

impl ShredDistributionJournal {
    pub const FLAG_SWAP_BYPASSED_BIT: usize = 0;
    pub const FLAG_SWEPT_BIT: usize = 1;

    #[inline]
    pub fn is_swap_bypassed(&self) -> bool {
        self.flags.bit(Self::FLAG_SWAP_BYPASSED_BIT)
    }

    #[inline]
    pub fn is_swept(&self) -> bool {
        self.flags.bit(Self::FLAG_SWEPT_BIT)
    }

    #[inline]
    pub fn checked_publisher_accumulation_bitmap_range(&self) -> Option<Range<usize>> {
        let has_end_index = self.publisher_accumulation_bitmap_end_index != 0;
        let range = self.publisher_accumulation_bitmap_start_index as usize
            ..self.publisher_accumulation_bitmap_end_index as usize;
        has_end_index.then_some(range)
    }

    #[inline]
    pub fn checked_client_accumulation_bitmap_range(&self) -> Option<Range<usize>> {
        let has_end_index = self.client_accumulation_bitmap_end_index != 0;
        let range = self.client_accumulation_bitmap_start_index as usize
            ..self.client_accumulation_bitmap_end_index as usize;
        has_end_index.then_some(range)
    }

    #[inline]
    pub fn checked_usdc_swap_budget(&self) -> Option<u64> {
        if self.total_leader_slots == 0 {
            return None;
        }
        let accumulated_slots_scaled =
            self.accumulated_publisher_slots_scaled + self.accumulated_client_slots_scaled;
        let total_slots_scaled = u64::from(self.total_leader_slots) * u64::from(UnitShare16::MAX);
        let budget = u128::from(self.validator_pool) * u128::from(accumulated_slots_scaled)
            / u128::from(total_slots_scaled);
        Some(budget as u64)
    }

    #[inline]
    pub fn is_swap_complete(&self) -> bool {
        if self.is_swap_bypassed() {
            return true;
        }
        match self.checked_usdc_swap_budget() {
            Some(budget) => self.usdc_swapped_amount == budget,
            None => true,
        }
    }
}

// Mirror the on-chain
// `assert!(zero_copy::data_end::<T>() == N)` lines in
// `programs/shred-subscription/src/processor/mod.rs`. `data_end` is
// `DISCRIMINATOR_LEN + size_of::<T>()`, so the offchain size assert is
// `size_of::<T>() == N - DISCRIMINATOR_LEN`. If on-chain bumps either
// value, update both sides together.
const _: () = assert!(std::mem::size_of::<ValidatorClientRewards>() == 184 - DISCRIMINATOR_LEN);
const _: () = assert!(std::mem::size_of::<ShredDistribution>() == 400 - DISCRIMINATOR_LEN);
const _: () = assert!(std::mem::size_of::<ShredDistributionJournal>() == 296 - DISCRIMINATOR_LEN);
// `ValidatorClientRewardsConfig` is a field inside `ShredDistribution`,
// not a top-level account, so the on-chain code has no separate
// `data_end::<T>()` assert for it. Pin its size here directly so any
// upstream layout shift breaks the build instead of silently relocating
// later fields of `ShredDistribution`.
const _: () = assert!(std::mem::size_of::<ValidatorClientRewardsConfig>() == 136);

#[cfg(test)]
mod tests {
    use super::*;

    fn program_config_data_with_flags(flags: u64) -> Vec<u8> {
        let mut data = vec![0u8; PROGRAM_CONFIG_FLAGS_OFFSET + 8];
        data[PROGRAM_CONFIG_FLAGS_OFFSET..PROGRAM_CONFIG_FLAGS_OFFSET + 8]
            .copy_from_slice(&flags.to_le_bytes());
        data
    }

    #[test]
    fn prorated_enabled_bit_set() {
        let data =
            program_config_data_with_flags(PROGRAM_CONFIG_FLAG_IS_PRORATED_SERVICE_ENABLED_BIT);
        assert!(is_prorated_service_enabled(&data));
    }

    #[test]
    fn prorated_enabled_bit_unset() {
        let data = program_config_data_with_flags(0);
        assert!(!is_prorated_service_enabled(&data));
    }

    #[test]
    fn prorated_enabled_other_bits_ignored() {
        // is_paused (bit 0) + is_migrated (bit 1) set, but not bit 2.
        let data = program_config_data_with_flags(0b011);
        assert!(!is_prorated_service_enabled(&data));
    }

    #[test]
    fn prorated_enabled_short_buffer_returns_false() {
        // Pre-prorated program deployment: account data too short for the
        // flags word. Treat as flag unset rather than panicking.
        let data = vec![0u8; PROGRAM_CONFIG_FLAGS_OFFSET + 4];
        assert!(!is_prorated_service_enabled(&data));
    }

    #[test]
    fn prorated_enabled_empty_buffer_returns_false() {
        assert!(!is_prorated_service_enabled(&[]));
    }

    #[test]
    fn distribute_validator_rewards_enabled_bit_set() {
        let data = program_config_data_with_flags(
            PROGRAM_CONFIG_FLAG_DISTRIBUTE_VALIDATOR_REWARDS_ENABLED_BIT,
        );
        assert!(is_distribute_validator_rewards_enabled(&data));
    }

    #[test]
    fn distribute_validator_rewards_disabled_when_unset() {
        // All lower bits set (paused, prorated, accumulate, jupiter) but
        // not bit 5 — must not be mistaken for distribute-enabled.
        let data = program_config_data_with_flags(0b01_1111);
        assert!(!is_distribute_validator_rewards_enabled(&data));
    }

    #[test]
    fn distribute_validator_rewards_enabled_short_buffer_returns_false() {
        let data = vec![0u8; PROGRAM_CONFIG_FLAGS_OFFSET + 4];
        assert!(!is_distribute_validator_rewards_enabled(&data));
    }

    fn client_seat_data_with_last_price(price_dollars: u16) -> Vec<u8> {
        let mut data = vec![0u8; CLIENT_SEAT_LAST_USDC_PRICE_OFFSET + 2];
        data[CLIENT_SEAT_LAST_USDC_PRICE_OFFSET..CLIENT_SEAT_LAST_USDC_PRICE_OFFSET + 2]
            .copy_from_slice(&price_dollars.to_le_bytes());
        data
    }

    #[test]
    fn last_usdc_price_zero() {
        let data = client_seat_data_with_last_price(0);
        assert_eq!(parse_client_seat_last_usdc_price_dollars(&data), Some(0));
    }

    #[test]
    fn last_usdc_price_nonzero() {
        let data = client_seat_data_with_last_price(42);
        assert_eq!(parse_client_seat_last_usdc_price_dollars(&data), Some(42));
    }

    #[test]
    fn last_usdc_price_short_buffer_returns_none() {
        let data = vec![0u8; CLIENT_SEAT_LAST_USDC_PRICE_OFFSET];
        assert_eq!(parse_client_seat_last_usdc_price_dollars(&data), None);
    }

    #[test]
    fn find_claim_holding_address_matches_seed() {
        use solana_sdk::pubkey::Pubkey;
        let parent = Pubkey::new_from_array([7u8; 32]);
        let mint = Pubkey::new_from_array([3u8; 32]);
        let epoch: u64 = 42;
        let (addr, bump) = find_claim_holding_address(&parent, epoch, &mint);
        let (expected_addr, expected_bump) = Pubkey::find_program_address(
            &[
                CLAIM_HOLDING_SEED_PREFIX,
                parent.as_ref(),
                &epoch.to_le_bytes(),
                mint.as_ref(),
            ],
            &crate::shred_subscription::ID,
        );
        assert_eq!(addr, expected_addr);
        assert_eq!(bump, expected_bump);
    }

    fn validator_client_rewards_with_description(description: &[u8]) -> ValidatorClientRewards {
        let mut validator_client_rewards = ValidatorClientRewards::default();
        validator_client_rewards.short_description_bytes[..description.len()]
            .copy_from_slice(description);
        validator_client_rewards
    }

    #[test]
    fn test_checked_short_description_returns_str() {
        let validator_client_rewards = validator_client_rewards_with_description(b"acme");
        assert_eq!(
            validator_client_rewards.checked_short_description(),
            Some("acme")
        );
    }

    #[test]
    fn test_checked_short_description_empty_returns_none() {
        let validator_client_rewards = ValidatorClientRewards::default();
        assert!(
            validator_client_rewards
                .checked_short_description()
                .is_none()
        );
    }

    #[test]
    fn test_checked_short_description_full_length() {
        // 64 is the width of short_description_bytes, so the fill leaves no
        // trailing zero to scan back from.
        let description = "a".repeat(64);
        let validator_client_rewards =
            validator_client_rewards_with_description(description.as_bytes());
        assert_eq!(
            validator_client_rewards.checked_short_description(),
            Some(description.as_str())
        );
    }

    #[test]
    fn parse_program_config_shred_oracle_key_happy_path() {
        use solana_sdk::pubkey::Pubkey;
        let oracle = Pubkey::new_from_array([5u8; 32]);
        let mut data = vec![0u8; PROGRAM_CONFIG_SHRED_ORACLE_KEY_OFFSET + 32];
        let disc_bytes =
            borsh::to_vec(&PROGRAM_CONFIG_DISCRIMINATOR).expect("discriminator serialization");
        data[..DISCRIMINATOR_LEN].copy_from_slice(&disc_bytes);
        data[PROGRAM_CONFIG_SHRED_ORACLE_KEY_OFFSET..PROGRAM_CONFIG_SHRED_ORACLE_KEY_OFFSET + 32]
            .copy_from_slice(oracle.as_ref());
        assert_eq!(parse_program_config_shred_oracle_key(&data), Some(oracle));
    }

    #[test]
    fn parse_program_config_shred_oracle_key_short_buffer_returns_none() {
        let data = vec![0u8; PROGRAM_CONFIG_SHRED_ORACLE_KEY_OFFSET + 31];
        assert_eq!(parse_program_config_shred_oracle_key(&data), None);
    }

    /// Build a `MetroHistory` buffer whose ring holds `entries` as
    /// `(ring_slot, epoch, usdc_price_dollars)`.
    fn metro_history_data(
        current_index: u8,
        total_count: u8,
        entries: &[(usize, u64, u16)],
    ) -> Vec<u8> {
        let mut data =
            vec![
                0;
                METRO_HISTORY_RING_OFFSET + 8 + RING_BUFFER_CAPACITY * METRO_HISTORY_ENTRY_SIZE
            ];
        data[METRO_HISTORY_RING_OFFSET] = current_index;
        data[METRO_HISTORY_RING_OFFSET + 1] = total_count;
        for (ring_slot, epoch, price_dollars) in entries {
            let offset = METRO_HISTORY_RING_OFFSET + 8 + ring_slot * METRO_HISTORY_ENTRY_SIZE;
            data[offset..offset + 8].copy_from_slice(&epoch.to_le_bytes());
            data[offset + 8..offset + 10].copy_from_slice(&price_dollars.to_le_bytes());
        }
        data
    }

    #[test]
    fn test_metro_price_at_epoch_exact_hit() {
        // Slots 0..3 written, newest at 2 (epoch 12).
        let data = metro_history_data(2, 3, &[(0, 10, 30), (1, 11, 43), (2, 12, 10)]);
        assert_eq!(parse_metro_history_price_at_epoch(&data, 12), Some(10));
        assert_eq!(parse_metro_history_price_at_epoch(&data, 11), Some(43));
        assert_eq!(parse_metro_history_price_at_epoch(&data, 10), Some(30));
    }

    #[test]
    fn test_metro_price_at_epoch_wraps_past_index_zero() {
        // current_index 0 with 3 written entries: the two older ones live in
        // slots 31 and 30, so the search has to wrap backwards.
        let data = metro_history_data(0, 3, &[(30, 10, 30), (31, 11, 43), (0, 12, 10)]);
        assert_eq!(parse_metro_history_price_at_epoch(&data, 11), Some(43));
        assert_eq!(parse_metro_history_price_at_epoch(&data, 10), Some(30));
    }

    #[test]
    fn test_metro_price_at_epoch_respects_total_count_bound() {
        // Epoch 9 sits in slot 31, one step beyond the two written entries.
        // The onchain `find` never reaches it, so neither may this.
        let data = metro_history_data(1, 2, &[(31, 9, 60), (0, 10, 30), (1, 11, 43)]);
        assert_eq!(parse_metro_history_price_at_epoch(&data, 11), Some(43));
        assert_eq!(parse_metro_history_price_at_epoch(&data, 10), Some(30));
        assert_eq!(parse_metro_history_price_at_epoch(&data, 9), None);
    }

    #[test]
    fn test_metro_price_at_epoch_zero_on_uninitialized_buffer() {
        // total_count 0: every slot is zeroed, and epoch 0 must not match.
        let data = metro_history_data(0, 0, &[]);
        assert_eq!(parse_metro_history_price_at_epoch(&data, 0), None);
    }

    #[test]
    fn test_metro_price_at_epoch_zero_matches_written_entry() {
        // Epoch 0 is a legitimate epoch once written.
        let data = metro_history_data(0, 1, &[(0, 0, 30)]);
        assert_eq!(parse_metro_history_price_at_epoch(&data, 0), Some(30));
    }

    #[test]
    fn test_metro_price_at_epoch_miss_returns_none() {
        let data = metro_history_data(1, 2, &[(0, 10, 30), (1, 11, 43)]);
        assert_eq!(parse_metro_history_price_at_epoch(&data, 12), None);
        assert_eq!(parse_metro_history_price_at_epoch(&data, 9), None);
    }

    #[test]
    fn test_metro_price_at_epoch_short_buffer_returns_none() {
        assert_eq!(parse_metro_history_price_at_epoch(&[], 10), None);
        let truncated = vec![0; METRO_HISTORY_RING_OFFSET];
        assert_eq!(parse_metro_history_price_at_epoch(&truncated, 10), None);
    }

    #[test]
    fn test_device_premium_at_epoch_reads_signed_premium() {
        let mut data =
            vec![
                0;
                DEVICE_HISTORY_RING_OFFSET + 8 + RING_BUFFER_CAPACITY * DEVICE_HISTORY_ENTRY_SIZE
            ];
        data[DEVICE_HISTORY_RING_OFFSET] = 1;
        data[DEVICE_HISTORY_RING_OFFSET + 1] = 2;
        for (ring_slot, epoch, premium_dollars) in [(0, 10, -5), (1, 11, 7)] {
            let offset = DEVICE_HISTORY_RING_OFFSET + 8 + ring_slot * DEVICE_HISTORY_ENTRY_SIZE;
            data[offset..offset + 8].copy_from_slice(&<u64>::to_le_bytes(epoch));
            data[offset + 8..offset + 10].copy_from_slice(&<i16>::to_le_bytes(premium_dollars));
        }
        assert_eq!(parse_device_history_premium_at_epoch(&data, 11), Some(7));
        assert_eq!(parse_device_history_premium_at_epoch(&data, 10), Some(-5));
        assert_eq!(parse_device_history_premium_at_epoch(&data, 12), None);
    }

    #[test]
    fn test_seat_usdc_price_dollars_applies_signed_premium() {
        assert_eq!(seat_usdc_price_dollars(30, 13), 43);
        assert_eq!(seat_usdc_price_dollars(30, -20), 10);
        assert_eq!(seat_usdc_price_dollars(30, 0), 30);
        // Floors at zero rather than wrapping.
        assert_eq!(seat_usdc_price_dollars(10, -30), 0);
        assert_eq!(seat_usdc_price_dollars(u16::MAX, 5), u16::MAX);
    }

    #[test]
    fn parse_program_config_shred_oracle_key_wrong_discriminator_returns_none() {
        use solana_sdk::pubkey::Pubkey;
        let oracle = Pubkey::new_from_array([5u8; 32]);
        let mut data = vec![0u8; PROGRAM_CONFIG_SHRED_ORACLE_KEY_OFFSET + 32];
        data[0] = 0x01;
        data[PROGRAM_CONFIG_SHRED_ORACLE_KEY_OFFSET..PROGRAM_CONFIG_SHRED_ORACLE_KEY_OFFSET + 32]
            .copy_from_slice(oracle.as_ref());
        assert_eq!(parse_program_config_shred_oracle_key(&data), None);
    }
}
