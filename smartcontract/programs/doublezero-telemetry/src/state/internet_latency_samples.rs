use crate::{
    seeds::SEED_INET_LATENCY_SAMPLES,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;
use std::{
    fmt,
    io::{self, Read, Write},
};

/// Maximum number of RTT samples storable in a single account
/// With 1-minute intervals, 3_000 samples ~= 48 hours of data.
pub const MAX_INTERNET_SAMPLES: usize = 3_000;

/// Static size of the `InternetLatencySamples` struct without the `samples` vec.
/// Used to calculate initial account allocation. Bytes per field:
/// - 1 byte: `account_type`
/// - 1 byte: `bump_seed`
/// - 32 bytes: `data_provider_name`
/// - 8 byte: `epoch`
/// - 3 * 32 bytes: pubkeys for `agent`, `locations`
/// - 8 bytes: `start_timestamp_microseconds`
/// - 4 bytes: `next_sample_index`
/// - 128 bytes: reserved for future use
///
/// Total size: 281 bytes
pub const INTERNET_LATENCY_SAMPLES_HEADER_SIZE: usize = 1 + 1 + 32 + 8 + 32 + 32 + 32 + 8 + 4 + 128;

/// Onchain data structure representing a latency samples account header between two
/// location over the public internet for a specific epoch and third party probe provider,
/// written by a single agent account managed by the serviceability global state.
#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone)]
pub struct InternetLatencySamplesHeader {
    //
}
