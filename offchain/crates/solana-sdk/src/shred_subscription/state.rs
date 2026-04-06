use std::net::Ipv4Addr;

use doublezero_program_tools::{DISCRIMINATOR_LEN, Discriminator};
use solana_sdk::pubkey::Pubkey;

pub const PROGRAM_CONFIG_SEED_PREFIX: &[u8] = b"program_config";
pub const EXECUTION_CONTROLLER_SEED_PREFIX: &[u8] = b"execution_controller";
pub const DEVICE_HISTORY_SEED_PREFIX: &[u8] = b"device_history";
pub const CLIENT_SEAT_SEED_PREFIX: &[u8] = b"client_seat";
pub const METRO_HISTORY_SEED_PREFIX: &[u8] = b"metro_history";
pub const TOKEN_PDA_SEED_PREFIX: &[u8] = b"token";
pub const PAYMENT_ESCROW_SEED_PREFIX: &[u8] = b"payment_escrow";
pub const VALIDATOR_CLIENT_REWARDS_SEED_PREFIX: &[u8] = b"validator_client_rewards";
pub const INSTANT_ALLOCATION_REQUEST_SEED_PREFIX: &[u8] = b"instant_seat_allocation_request";
pub const WITHDRAW_SEAT_REQUEST_SEED_PREFIX: &[u8] = b"withdraw_seat_request";

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
