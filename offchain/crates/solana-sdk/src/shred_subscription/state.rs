use std::net::Ipv4Addr;

use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{
    DISCRIMINATOR_LEN, Discriminator, PrecomputedDiscriminator,
    types::{Flags, StorageGap},
};
use doublezero_revenue_distribution::types::UnitShare16;
use solana_sdk::pubkey::Pubkey;

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

// ---------------------------------------------------------------------------
// ValidatorClientRewards raw-byte parsing.
//
// Layout (Pod with 8-byte discriminator prefix):
//   [0..8)     discriminator
//   [8..10)    client_id: u16
//   [10..11)   bump_seed: u8
//   [11..16)   _padding_0: [u8; 5]
//   [16..48)   manager_key: Pubkey
//   [48..112)  short_description_bytes: [u8; 64]
//   [112..116) claim_holding_count: u32
//   ...        remaining fields (padding + StorageGap) unused by the CLI
// ---------------------------------------------------------------------------

pub const VALIDATOR_CLIENT_REWARDS_DISCRIMINATOR: Discriminator<DISCRIMINATOR_LEN> =
    Discriminator::new_sha2(b"dz::account::validator_client_rewards");

pub const VCR_CLIENT_ID_OFFSET: usize = DISCRIMINATOR_LEN;
pub const VCR_BUMP_SEED_OFFSET: usize = DISCRIMINATOR_LEN + 2;
pub const VCR_MANAGER_KEY_OFFSET: usize = DISCRIMINATOR_LEN + 8;
pub const VCR_SHORT_DESCRIPTION_OFFSET: usize = DISCRIMINATOR_LEN + 40;
pub const VCR_CLAIM_HOLDING_COUNT_OFFSET: usize = DISCRIMINATOR_LEN + 104;
pub const VCR_SHORT_DESCRIPTION_LEN: usize = 64;
/// Total on-chain size of a `ValidatorClientRewards` account, including the
/// 8-byte discriminator. Mirrors the program's
/// `assert!(zero_copy::data_end::<ValidatorClientRewards>() == 184)`.
/// Update both sides together if the on-chain layout changes.
pub const VCR_ACCOUNT_DATA_LEN: usize = 184;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ValidatorClientRewardsInfo {
    pub client_id: u16,
    pub manager_key: Pubkey,
    pub short_description: Option<String>,
    pub claim_holding_count: u32,
}

/// Parse a `ValidatorClientRewards` from raw account data. Returns `None`
/// when the data is too short or the discriminator does not match.
pub fn parse_validator_client_rewards(data: &[u8]) -> Option<ValidatorClientRewardsInfo> {
    if data.len() < VCR_CLAIM_HOLDING_COUNT_OFFSET + 4 {
        return None;
    }
    let expected_disc = borsh::to_vec(&VALIDATOR_CLIENT_REWARDS_DISCRIMINATOR)
        .expect("discriminator serialization");
    if data[..DISCRIMINATOR_LEN] != expected_disc[..] {
        return None;
    }
    let client_id = u16::from_le_bytes(
        data[VCR_CLIENT_ID_OFFSET..VCR_CLIENT_ID_OFFSET + 2]
            .try_into()
            .ok()?,
    );
    let manager_key = Pubkey::new_from_array(
        data[VCR_MANAGER_KEY_OFFSET..VCR_MANAGER_KEY_OFFSET + 32]
            .try_into()
            .ok()?,
    );
    let short_description_bytes = &data
        [VCR_SHORT_DESCRIPTION_OFFSET..VCR_SHORT_DESCRIPTION_OFFSET + VCR_SHORT_DESCRIPTION_LEN];
    let short_description = match short_description_bytes.iter().rposition(|&b| b != 0) {
        Some(end) => std::str::from_utf8(&short_description_bytes[..=end])
            .ok()
            .map(str::to_string),
        None => None,
    };
    let claim_holding_count = u32::from_le_bytes(
        data[VCR_CLAIM_HOLDING_COUNT_OFFSET..VCR_CLAIM_HOLDING_COUNT_OFFSET + 4]
            .try_into()
            .ok()?,
    );
    Some(ValidatorClientRewardsInfo {
        client_id,
        manager_key,
        short_description,
        claim_holding_count,
    })
}

// ---------------------------------------------------------------------------
// ShredRewardToken and ValidatorPublisherRewards: layout mirrored from
// the on-chain `doublezero-shred-subscription` program (state module).
// Kept here to avoid pulling the program crate as a dependency just for two
// account types. If the on-chain layout changes, update both this file and
// the discriminator strings together.
// ---------------------------------------------------------------------------

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

    fn vcr_data(client_id: u16, manager: Pubkey, desc: &[u8], count: u32) -> Vec<u8> {
        let mut data = vec![0u8; VCR_CLAIM_HOLDING_COUNT_OFFSET + 4];
        let disc_bytes = borsh::to_vec(&VALIDATOR_CLIENT_REWARDS_DISCRIMINATOR)
            .expect("discriminator serialization");
        data[..DISCRIMINATOR_LEN].copy_from_slice(&disc_bytes);
        data[VCR_CLIENT_ID_OFFSET..VCR_CLIENT_ID_OFFSET + 2]
            .copy_from_slice(&client_id.to_le_bytes());
        data[VCR_MANAGER_KEY_OFFSET..VCR_MANAGER_KEY_OFFSET + 32].copy_from_slice(manager.as_ref());
        let desc_end = VCR_SHORT_DESCRIPTION_OFFSET + desc.len();
        data[VCR_SHORT_DESCRIPTION_OFFSET..desc_end].copy_from_slice(desc);
        data[VCR_CLAIM_HOLDING_COUNT_OFFSET..VCR_CLAIM_HOLDING_COUNT_OFFSET + 4]
            .copy_from_slice(&count.to_le_bytes());
        data
    }

    #[test]
    fn parse_validator_client_rewards_happy_path() {
        use solana_sdk::pubkey::Pubkey;
        let manager = Pubkey::new_from_array([11u8; 32]);
        let data = vcr_data(42, manager, b"acme", 3);
        let info = parse_validator_client_rewards(&data).expect("parse");
        assert_eq!(info.client_id, 42);
        assert_eq!(info.manager_key, manager);
        assert_eq!(info.short_description.as_deref(), Some("acme"));
        assert_eq!(info.claim_holding_count, 3);
    }

    #[test]
    fn parse_validator_client_rewards_empty_description_returns_none_description() {
        use solana_sdk::pubkey::Pubkey;
        let data = vcr_data(0, Pubkey::default(), b"", 0);
        let info = parse_validator_client_rewards(&data).expect("parse");
        assert!(info.short_description.is_none());
    }

    #[test]
    fn parse_validator_client_rewards_short_buffer_returns_none() {
        let data = vec![0u8; VCR_CLAIM_HOLDING_COUNT_OFFSET + 3];
        assert!(parse_validator_client_rewards(&data).is_none());
    }

    #[test]
    fn parse_validator_client_rewards_wrong_discriminator_returns_none() {
        use solana_sdk::pubkey::Pubkey;
        let mut data = vcr_data(1, Pubkey::default(), b"x", 0);
        data[0] ^= 0xff;
        assert!(parse_validator_client_rewards(&data).is_none());
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
