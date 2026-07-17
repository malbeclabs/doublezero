//! Generates Borsh-serialized binary fixture files from the Rust geolocation structs
//! with known field values. The Go SDK fixture tests deserialize these fixtures and
//! verify that field values match the expected values from the JSON sidecar files.
//!
//! Run with: cargo run (from this directory)
//! Output: ../*.bin and ../*.json

use std::fs;
use std::net::Ipv4Addr;
use std::path::Path;

use doublezero_geolocation::state::{
    accounttype::AccountType,
    geo_probe::GeoProbe,
    geolocation_user::{
        FlatPerEpochConfig, GeoLocationTargetType, GeolocationBillingConfig,
        GeolocationPaymentStatus, GeolocationTarget, GeolocationUser, GeolocationUserStatus,
    },
    program_config::GeolocationProgramConfig,
};
use serde::Serialize;

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

fn pubkey_from_byte(b: u8) -> solana_program::pubkey::Pubkey {
    let mut bytes = [0u8; 32];
    bytes[0] = b;
    solana_program::pubkey::Pubkey::new_from_array(bytes)
}

fn pubkey_bs58(pk: &solana_program::pubkey::Pubkey) -> String {
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

    generate_program_config(&fixtures_dir);
    generate_geo_probe(&fixtures_dir);
    generate_geolocation_user(&fixtures_dir);

    println!("\nall fixtures generated in {}", fixtures_dir.display());
}

fn generate_program_config(dir: &Path) {
    let val = GeolocationProgramConfig {
        account_type: AccountType::ProgramConfig,
        bump_seed: 254,
        version: 3,
        min_compatible_version: 1,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "GeolocationProgramConfig".into(),
        account_type: AccountType::ProgramConfig as u8,
        fields: vec![
            FieldValue {
                name: "AccountType".into(),
                value: "1".into(),
                typ: "u8".into(),
            },
            FieldValue {
                name: "BumpSeed".into(),
                value: "254".into(),
                typ: "u8".into(),
            },
            FieldValue {
                name: "Version".into(),
                value: "3".into(),
                typ: "u32".into(),
            },
            FieldValue {
                name: "MinCompatibleVersion".into(),
                value: "1".into(),
                typ: "u32".into(),
            },
        ],
    };

    write_fixture(dir, "program_config", &data, &meta);
}

fn generate_geo_probe(dir: &Path) {
    let owner = pubkey_from_byte(0x10);
    let exchange_pk = pubkey_from_byte(0x11);
    let metrics_publisher_pk = pubkey_from_byte(0x12);
    let parent0 = pubkey_from_byte(0x20);
    let parent1 = pubkey_from_byte(0x21);

    let val = GeoProbe {
        account_type: AccountType::GeoProbe,
        owner,
        exchange_pk,
        public_ip: Ipv4Addr::new(203, 0, 113, 7),
        location_offset_port: 8923,
        metrics_publisher_pk,
        reference_count: 3,
        code: "probe-ams-01".into(),
        parent_devices: vec![parent0, parent1],
        target_update_count: 42,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "GeoProbe".into(),
        account_type: AccountType::GeoProbe as u8,
        fields: vec![
            FieldValue {
                name: "AccountType".into(),
                value: "2".into(),
                typ: "u8".into(),
            },
            FieldValue {
                name: "Owner".into(),
                value: pubkey_bs58(&owner),
                typ: "pubkey".into(),
            },
            FieldValue {
                name: "ExchangePK".into(),
                value: pubkey_bs58(&exchange_pk),
                typ: "pubkey".into(),
            },
            FieldValue {
                name: "PublicIP".into(),
                value: "203.0.113.7".into(),
                typ: "ipv4".into(),
            },
            FieldValue {
                name: "LocationOffsetPort".into(),
                value: "8923".into(),
                typ: "u16".into(),
            },
            FieldValue {
                name: "MetricsPublisherPK".into(),
                value: pubkey_bs58(&metrics_publisher_pk),
                typ: "pubkey".into(),
            },
            FieldValue {
                name: "ReferenceCount".into(),
                value: "3".into(),
                typ: "u32".into(),
            },
            FieldValue {
                name: "Code".into(),
                value: "probe-ams-01".into(),
                typ: "string".into(),
            },
            FieldValue {
                name: "ParentDevicesLen".into(),
                value: "2".into(),
                typ: "u32".into(),
            },
            FieldValue {
                name: "ParentDevices0".into(),
                value: pubkey_bs58(&parent0),
                typ: "pubkey".into(),
            },
            FieldValue {
                name: "ParentDevices1".into(),
                value: pubkey_bs58(&parent1),
                typ: "pubkey".into(),
            },
            FieldValue {
                name: "TargetUpdateCount".into(),
                value: "42".into(),
                typ: "u32".into(),
            },
        ],
    };

    write_fixture(dir, "geo_probe", &data, &meta);
}

fn generate_geolocation_user(dir: &Path) {
    let owner = pubkey_from_byte(0x30);
    let token_account = pubkey_from_byte(0x31);
    let inbound_target_pk = pubkey_from_byte(0x32);
    let geoprobe_pk = pubkey_from_byte(0x33);

    let outbound = GeolocationTarget {
        target_type: GeoLocationTargetType::Outbound,
        ip_address: Ipv4Addr::new(8, 8, 8, 8),
        location_offset_port: 8923,
        target_pk: solana_program::pubkey::Pubkey::default(),
        geoprobe_pk,
    };

    let inbound = GeolocationTarget {
        target_type: GeoLocationTargetType::Inbound,
        ip_address: Ipv4Addr::UNSPECIFIED,
        location_offset_port: 0,
        target_pk: inbound_target_pk,
        geoprobe_pk,
    };

    let outbound_icmp = GeolocationTarget {
        target_type: GeoLocationTargetType::OutboundIcmp,
        ip_address: Ipv4Addr::new(1, 1, 1, 1),
        location_offset_port: 0, // common in practice; delivery may be overridden
        target_pk: solana_program::pubkey::Pubkey::default(),
        geoprobe_pk,
    };

    let val = GeolocationUser {
        account_type: AccountType::GeolocationUser,
        owner,
        code: "geo-user-01".into(),
        token_account,
        payment_status: GeolocationPaymentStatus::Paid,
        billing: GeolocationBillingConfig::FlatPerEpoch(FlatPerEpochConfig {
            rate: 1000,
            last_deduction_dz_epoch: 42,
        }),
        status: GeolocationUserStatus::Activated,
        targets: vec![outbound, inbound, outbound_icmp],
        result_destination: "results.example.com:9000".into(),
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "GeolocationUser".into(),
        account_type: AccountType::GeolocationUser as u8,
        fields: vec![
            FieldValue {
                name: "AccountType".into(),
                value: "3".into(),
                typ: "u8".into(),
            },
            FieldValue {
                name: "Owner".into(),
                value: pubkey_bs58(&owner),
                typ: "pubkey".into(),
            },
            FieldValue {
                name: "Code".into(),
                value: "geo-user-01".into(),
                typ: "string".into(),
            },
            FieldValue {
                name: "TokenAccount".into(),
                value: pubkey_bs58(&token_account),
                typ: "pubkey".into(),
            },
            FieldValue {
                name: "PaymentStatus".into(),
                value: "1".into(),
                typ: "u8".into(),
            },
            FieldValue {
                name: "BillingDiscriminant".into(),
                value: "0".into(),
                typ: "u8".into(),
            },
            FieldValue {
                name: "BillingRate".into(),
                value: "1000".into(),
                typ: "u64".into(),
            },
            FieldValue {
                name: "BillingLastDeductionDzEpoch".into(),
                value: "42".into(),
                typ: "u64".into(),
            },
            FieldValue {
                name: "Status".into(),
                value: "0".into(),
                typ: "u8".into(),
            },
            FieldValue {
                name: "TargetsLen".into(),
                value: "3".into(),
                typ: "u32".into(),
            },
            // Target 0: outbound
            FieldValue {
                name: "Target0Type".into(),
                value: "0".into(),
                typ: "u8".into(),
            },
            FieldValue {
                name: "Target0IP".into(),
                value: "8.8.8.8".into(),
                typ: "ipv4".into(),
            },
            FieldValue {
                name: "Target0LocationOffsetPort".into(),
                value: "8923".into(),
                typ: "u16".into(),
            },
            FieldValue {
                name: "Target0TargetPK".into(),
                value: pubkey_bs58(&solana_program::pubkey::Pubkey::default()),
                typ: "pubkey".into(),
            },
            FieldValue {
                name: "Target0GeoProbePK".into(),
                value: pubkey_bs58(&geoprobe_pk),
                typ: "pubkey".into(),
            },
            // Target 1: inbound
            FieldValue {
                name: "Target1Type".into(),
                value: "1".into(),
                typ: "u8".into(),
            },
            FieldValue {
                name: "Target1IP".into(),
                value: "0.0.0.0".into(),
                typ: "ipv4".into(),
            },
            FieldValue {
                name: "Target1LocationOffsetPort".into(),
                value: "0".into(),
                typ: "u16".into(),
            },
            FieldValue {
                name: "Target1TargetPK".into(),
                value: pubkey_bs58(&inbound_target_pk),
                typ: "pubkey".into(),
            },
            FieldValue {
                name: "Target1GeoProbePK".into(),
                value: pubkey_bs58(&geoprobe_pk),
                typ: "pubkey".into(),
            },
            // Target 2: outbound ICMP
            FieldValue {
                name: "Target2Type".into(),
                value: "2".into(),
                typ: "u8".into(),
            },
            FieldValue {
                name: "Target2IP".into(),
                value: "1.1.1.1".into(),
                typ: "ipv4".into(),
            },
            FieldValue {
                name: "Target2LocationOffsetPort".into(),
                value: "0".into(),
                typ: "u16".into(),
            },
            FieldValue {
                name: "Target2TargetPK".into(),
                value: pubkey_bs58(&solana_program::pubkey::Pubkey::default()),
                typ: "pubkey".into(),
            },
            FieldValue {
                name: "Target2GeoProbePK".into(),
                value: pubkey_bs58(&geoprobe_pk),
                typ: "pubkey".into(),
            },
            FieldValue {
                name: "ResultDestination".into(),
                value: "results.example.com:9000".into(),
                typ: "string".into(),
            },
        ],
    };

    write_fixture(dir, "geolocation_user", &data, &meta);
}
