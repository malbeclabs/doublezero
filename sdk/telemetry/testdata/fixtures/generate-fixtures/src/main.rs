//! Generates Borsh-serialized binary fixture files from the Rust telemetry structs
//! with known field values. The Go/TypeScript/Python SDK compatibility tests deserialize
//! these fixtures and verify that field values match.
//!
//! Run with: cargo run (from this directory)
//! Output: ../fixtures/*.bin and ../fixtures/*.json

use std::fs;
use std::path::Path;

use borsh::BorshSerialize;
use doublezero_telemetry::state::{
    accounttype::AccountType,
    device_latency_samples::{DeviceLatencySamples, DeviceLatencySamplesHeader},
    internet_latency_samples::{InternetLatencySamples, InternetLatencySamplesHeader},
};
use serde::Serialize;
use solana_program::pubkey::Pubkey;

#[derive(Serialize)]
struct FixtureMeta {
    name: String,
    account_type: u8,
    fields: Vec<FieldValue>,
}

#[derive(Serialize)]
struct FieldValue {
    name: String,
    value: String,
    #[serde(rename = "typ")]
    typ: String,
}

fn pubkey_from_byte(b: u8) -> Pubkey {
    let mut bytes = [0u8; 32];
    bytes[0] = b;
    Pubkey::new_from_array(bytes)
}

fn pubkey_bs58(pk: &Pubkey) -> String {
    pk.to_string()
}

fn write_fixture(dir: &Path, name: &str, data: &[u8], meta: &FixtureMeta) {
    fs::write(dir.join(format!("{name}.bin")), data).unwrap();
    let json = serde_json::to_string_pretty(meta).unwrap();
    fs::write(dir.join(format!("{name}.json")), json).unwrap();
    println!("wrote {name}.bin ({} bytes) and {name}.json", data.len());
}

fn main() {
    let fixtures_dir = Path::new(env!("CARGO_MANIFEST_DIR")).join("..");
    fs::create_dir_all(&fixtures_dir).unwrap();

    generate_device_latency_samples(&fixtures_dir);
    generate_internet_latency_samples(&fixtures_dir);

    println!("\nall fixtures generated in {}", fixtures_dir.display());
}

fn generate_device_latency_samples(dir: &Path) {
    let agent_pk = pubkey_from_byte(0x01);
    let origin_pk = pubkey_from_byte(0x02);
    let target_pk = pubkey_from_byte(0x03);
    let origin_loc_pk = pubkey_from_byte(0x04);
    let target_loc_pk = pubkey_from_byte(0x05);
    let link_pk = pubkey_from_byte(0x06);

    let samples: Vec<u32> = vec![100, 200, 300, 400, 500];
    let val = DeviceLatencySamples {
        header: DeviceLatencySamplesHeader {
            account_type: AccountType::DeviceLatencySamples,
            epoch: 19800,
            origin_device_agent_pk: agent_pk,
            origin_device_pk: origin_pk,
            target_device_pk: target_pk,
            origin_device_location_pk: origin_loc_pk,
            target_device_location_pk: target_loc_pk,
            link_pk,
            sampling_interval_microseconds: 5_000_000,
            start_timestamp_microseconds: 1_700_000_000_000_000,
            next_sample_index: samples.len() as u32,
            _unused: [0; 128],
        },
        samples,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "device_latency_samples".to_string(),
        account_type: AccountType::DeviceLatencySamples as u8,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "3".into(), typ: "u8".into() },
            FieldValue { name: "Epoch".into(), value: "19800".into(), typ: "u64".into() },
            FieldValue { name: "OriginDeviceAgentPK".into(), value: pubkey_bs58(&agent_pk), typ: "pubkey".into() },
            FieldValue { name: "OriginDevicePK".into(), value: pubkey_bs58(&origin_pk), typ: "pubkey".into() },
            FieldValue { name: "TargetDevicePK".into(), value: pubkey_bs58(&target_pk), typ: "pubkey".into() },
            FieldValue { name: "OriginDeviceLocationPK".into(), value: pubkey_bs58(&origin_loc_pk), typ: "pubkey".into() },
            FieldValue { name: "TargetDeviceLocationPK".into(), value: pubkey_bs58(&target_loc_pk), typ: "pubkey".into() },
            FieldValue { name: "LinkPK".into(), value: pubkey_bs58(&link_pk), typ: "pubkey".into() },
            FieldValue { name: "SamplingIntervalMicroseconds".into(), value: "5000000".into(), typ: "u64".into() },
            FieldValue { name: "StartTimestampMicroseconds".into(), value: "1700000000000000".into(), typ: "u64".into() },
            FieldValue { name: "NextSampleIndex".into(), value: "5".into(), typ: "u32".into() },
            FieldValue { name: "SamplesCount".into(), value: "5".into(), typ: "u32".into() },
        ],
    };

    write_fixture(dir, "device_latency_samples", &data, &meta);
}

fn generate_internet_latency_samples(dir: &Path) {
    let oracle_pk = pubkey_from_byte(0x11);
    let origin_exchange_pk = pubkey_from_byte(0x12);
    let target_exchange_pk = pubkey_from_byte(0x13);

    let samples: Vec<u32> = vec![1000, 2000, 3000, 4000, 5000];
    let val = InternetLatencySamples {
        header: InternetLatencySamplesHeader {
            account_type: AccountType::InternetLatencySamples,
            epoch: 19800,
            data_provider_name: "RIPE Atlas".to_string(),
            oracle_agent_pk: oracle_pk,
            origin_exchange_pk,
            target_exchange_pk,
            sampling_interval_microseconds: 60_000_000,
            start_timestamp_microseconds: 1_700_000_000_000_000,
            next_sample_index: samples.len() as u32,
            _unused: [0; 128],
        },
        samples,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "internet_latency_samples".to_string(),
        account_type: AccountType::InternetLatencySamples as u8,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "4".into(), typ: "u8".into() },
            FieldValue { name: "Epoch".into(), value: "19800".into(), typ: "u64".into() },
            FieldValue { name: "DataProviderName".into(), value: "RIPE Atlas".into(), typ: "string".into() },
            FieldValue { name: "OracleAgentPK".into(), value: pubkey_bs58(&oracle_pk), typ: "pubkey".into() },
            FieldValue { name: "OriginExchangePK".into(), value: pubkey_bs58(&origin_exchange_pk), typ: "pubkey".into() },
            FieldValue { name: "TargetExchangePK".into(), value: pubkey_bs58(&target_exchange_pk), typ: "pubkey".into() },
            FieldValue { name: "SamplingIntervalMicroseconds".into(), value: "60000000".into(), typ: "u64".into() },
            FieldValue { name: "StartTimestampMicroseconds".into(), value: "1700000000000000".into(), typ: "u64".into() },
            FieldValue { name: "NextSampleIndex".into(), value: "5".into(), typ: "u32".into() },
            FieldValue { name: "SamplesCount".into(), value: "5".into(), typ: "u32".into() },
        ],
    };

    write_fixture(dir, "internet_latency_samples", &data, &meta);
}
