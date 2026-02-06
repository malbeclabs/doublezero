//! Generates Borsh-serialized binary fixture files from the Rust serviceability structs
//! with known field values. The Go/TypeScript/Python SDK compatibility tests deserialize
//! these fixtures and verify that field values match.
//!
//! Run with: cargo run (from this directory)
//! Output: ../fixtures/*.bin and ../fixtures/*.json
//!
//! Key difference from revdist fixtures: these use Borsh serialization (not repr(C)/bytemuck),
//! and the 1-byte AccountType discriminator is the first byte of the Borsh serialization itself
//! (no separate 8-byte discriminator prefix).

use std::fs;
use std::net::Ipv4Addr;
use std::path::Path;

use doublezero_serviceability::id_allocator::IdAllocator;
use doublezero_serviceability::ip_allocator::IpAllocator;
use doublezero_serviceability::programversion::ProgramVersion;
use doublezero_serviceability::state::{
    accesspass::{AccessPass, AccessPassStatus, AccessPassType},
    accounttype::AccountType,
    contributor::{Contributor, ContributorStatus},
    device::{Device, DeviceDesiredStatus, DeviceHealth, DeviceStatus, DeviceType},
    exchange::{Exchange, ExchangeStatus},
    globalconfig::GlobalConfig,
    globalstate::GlobalState,
    interface::{
        Interface, InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType, InterfaceV1,
        InterfaceV2, LoopbackType, RoutingMode,
    },
    link::{Link, LinkDesiredStatus, LinkHealth, LinkLinkType, LinkStatus},
    location::{Location, LocationStatus},
    multicastgroup::{MulticastGroup, MulticastGroupStatus},
    programconfig::ProgramConfig,
    user::{User, UserCYOA, UserStatus, UserType},
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

    generate_global_state(&fixtures_dir);
    generate_global_config(&fixtures_dir);
    generate_location(&fixtures_dir);
    generate_exchange(&fixtures_dir);
    generate_device(&fixtures_dir);
    generate_link(&fixtures_dir);
    generate_user(&fixtures_dir);
    generate_multicast_group(&fixtures_dir);
    generate_program_config(&fixtures_dir);
    generate_contributor(&fixtures_dir);
    generate_access_pass(&fixtures_dir);
    generate_access_pass_validator(&fixtures_dir);
    generate_resource_extension_id(&fixtures_dir);
    generate_resource_extension_ip(&fixtures_dir);

    println!("
all fixtures generated in {}", fixtures_dir.display());
}

fn generate_global_state(dir: &Path) {
    let foundation_pk = pubkey_from_byte(0x01);
    let activator_pk = pubkey_from_byte(0x02);
    let sentinel_pk = pubkey_from_byte(0x03);
    let health_oracle_pk = pubkey_from_byte(0x04);
    let qa_pk = pubkey_from_byte(0x05);

    let val = GlobalState {
        account_type: AccountType::GlobalState,
        bump_seed: 254,
        account_index: 42,
        foundation_allowlist: vec![foundation_pk],
        _device_allowlist: vec![],
        _user_allowlist: vec![],
        activator_authority_pk: activator_pk,
        sentinel_authority_pk: sentinel_pk,
        contributor_airdrop_lamports: 1_000_000_000,
        user_airdrop_lamports: 50_000,
        health_oracle_pk,
        qa_allowlist: vec![qa_pk],
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "GlobalState".into(),
        account_type: 1,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "BumpSeed".into(), value: "254".into(), typ: "u8".into() },
            FieldValue { name: "AccountIndex".into(), value: "42".into(), typ: "u128".into() },
            FieldValue { name: "FoundationAllowlistLen".into(), value: "1".into(), typ: "u32".into() },
            FieldValue { name: "FoundationAllowlist0".into(), value: pubkey_bs58(&foundation_pk), typ: "pubkey".into() },
            FieldValue { name: "DeviceAllowlistLen".into(), value: "0".into(), typ: "u32".into() },
            FieldValue { name: "UserAllowlistLen".into(), value: "0".into(), typ: "u32".into() },
            FieldValue { name: "ActivatorAuthorityPk".into(), value: pubkey_bs58(&activator_pk), typ: "pubkey".into() },
            FieldValue { name: "SentinelAuthorityPk".into(), value: pubkey_bs58(&sentinel_pk), typ: "pubkey".into() },
            FieldValue { name: "ContributorAirdropLamports".into(), value: "1000000000".into(), typ: "u64".into() },
            FieldValue { name: "UserAirdropLamports".into(), value: "50000".into(), typ: "u64".into() },
            FieldValue { name: "HealthOraclePk".into(), value: pubkey_bs58(&health_oracle_pk), typ: "pubkey".into() },
            FieldValue { name: "QaAllowlistLen".into(), value: "1".into(), typ: "u32".into() },
            FieldValue { name: "QaAllowlist0".into(), value: pubkey_bs58(&qa_pk), typ: "pubkey".into() },
        ],
    };

    write_fixture(dir, "global_state", &data, &meta);
}

fn generate_global_config(dir: &Path) {
    let owner = pubkey_from_byte(0x10);

    let val = GlobalConfig {
        account_type: AccountType::GlobalConfig,
        owner,
        bump_seed: 253,
        local_asn: 65000,
        remote_asn: 65001,
        device_tunnel_block: "10.100.0.0/16".parse().unwrap(),
        user_tunnel_block: "10.200.0.0/16".parse().unwrap(),
        multicastgroup_block: "239.0.0.0/8".parse().unwrap(),
        next_bgp_community: 10042,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "GlobalConfig".into(),
        account_type: 2,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "2".into(), typ: "u8".into() },
            FieldValue { name: "Owner".into(), value: pubkey_bs58(&owner), typ: "pubkey".into() },
            FieldValue { name: "BumpSeed".into(), value: "253".into(), typ: "u8".into() },
            FieldValue { name: "LocalAsn".into(), value: "65000".into(), typ: "u32".into() },
            FieldValue { name: "RemoteAsn".into(), value: "65001".into(), typ: "u32".into() },
            FieldValue { name: "DeviceTunnelBlock".into(), value: "10.100.0.0/16".into(), typ: "networkv4".into() },
            FieldValue { name: "UserTunnelBlock".into(), value: "10.200.0.0/16".into(), typ: "networkv4".into() },
            FieldValue { name: "MulticastGroupBlock".into(), value: "239.0.0.0/8".into(), typ: "networkv4".into() },
            FieldValue { name: "NextBgpCommunity".into(), value: "10042".into(), typ: "u16".into() },
        ],
    };

    write_fixture(dir, "global_config", &data, &meta);
}

fn generate_location(dir: &Path) {
    let owner = pubkey_from_byte(0x20);

    let val = Location {
        account_type: AccountType::Location,
        owner,
        index: 4,
        bump_seed: 252,
        lat: 52.3676,
        lng: 4.9041,
        loc_id: 4818,
        status: LocationStatus::Activated,
        code: "ams".into(),
        name: "Amsterdam".into(),
        country: "NL".into(),
        reference_count: 3,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "Location".into(),
        account_type: 3,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "3".into(), typ: "u8".into() },
            FieldValue { name: "Owner".into(), value: pubkey_bs58(&owner), typ: "pubkey".into() },
            FieldValue { name: "Index".into(), value: "4".into(), typ: "u128".into() },
            FieldValue { name: "BumpSeed".into(), value: "252".into(), typ: "u8".into() },
            FieldValue { name: "Lat".into(), value: "52.3676".into(), typ: "f64".into() },
            FieldValue { name: "Lng".into(), value: "4.9041".into(), typ: "f64".into() },
            FieldValue { name: "LocId".into(), value: "4818".into(), typ: "u32".into() },
            FieldValue { name: "Status".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Code".into(), value: "ams".into(), typ: "string".into() },
            FieldValue { name: "Name".into(), value: "Amsterdam".into(), typ: "string".into() },
            FieldValue { name: "Country".into(), value: "NL".into(), typ: "string".into() },
            FieldValue { name: "ReferenceCount".into(), value: "3".into(), typ: "u32".into() },
        ],
    };

    write_fixture(dir, "location", &data, &meta);
}

fn generate_exchange(dir: &Path) {
    let owner = pubkey_from_byte(0x30);
    let device1_pk = pubkey_from_byte(0x31);
    let device2_pk = pubkey_from_byte(0x32);

    let val = Exchange {
        account_type: AccountType::Exchange,
        owner,
        index: 12,
        bump_seed: 251,
        lat: 52.3676,
        lng: 4.9041,
        bgp_community: 10100,
        unused: 0,
        status: ExchangeStatus::Activated,
        code: "xams".into(),
        name: "Amsterdam IX".into(),
        reference_count: 5,
        device1_pk,
        device2_pk,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "Exchange".into(),
        account_type: 4,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "4".into(), typ: "u8".into() },
            FieldValue { name: "Owner".into(), value: pubkey_bs58(&owner), typ: "pubkey".into() },
            FieldValue { name: "Index".into(), value: "12".into(), typ: "u128".into() },
            FieldValue { name: "BumpSeed".into(), value: "251".into(), typ: "u8".into() },
            FieldValue { name: "Lat".into(), value: "52.3676".into(), typ: "f64".into() },
            FieldValue { name: "Lng".into(), value: "4.9041".into(), typ: "f64".into() },
            FieldValue { name: "BgpCommunity".into(), value: "10100".into(), typ: "u16".into() },
            FieldValue { name: "Unused".into(), value: "0".into(), typ: "u16".into() },
            FieldValue { name: "Status".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Code".into(), value: "xams".into(), typ: "string".into() },
            FieldValue { name: "Name".into(), value: "Amsterdam IX".into(), typ: "string".into() },
            FieldValue { name: "ReferenceCount".into(), value: "5".into(), typ: "u32".into() },
            FieldValue { name: "Device1Pk".into(), value: pubkey_bs58(&device1_pk), typ: "pubkey".into() },
            FieldValue { name: "Device2Pk".into(), value: pubkey_bs58(&device2_pk), typ: "pubkey".into() },
        ],
    };

    write_fixture(dir, "exchange", &data, &meta);
}

fn generate_device(dir: &Path) {
    let owner = pubkey_from_byte(0x40);
    let location_pk = pubkey_from_byte(0x41);
    let exchange_pk = pubkey_from_byte(0x42);
    let metrics_publisher_pk = pubkey_from_byte(0x43);
    let contributor_pk = pubkey_from_byte(0x44);

    let val = Device {
        account_type: AccountType::Device,
        owner,
        index: 7,
        bump_seed: 250,
        location_pk,
        exchange_pk,
        device_type: DeviceType::Edge,
        public_ip: Ipv4Addr::new(203, 0, 113, 1),
        status: DeviceStatus::Activated,
        code: "dz1".into(),
        dz_prefixes: vec!["10.10.0.0/24".parse().unwrap()].into(),
        metrics_publisher_pk,
        contributor_pk,
        mgmt_vrf: "mgmt".into(),
        interfaces: vec![
            Interface::V1(InterfaceV1 {
                status: InterfaceStatus::Activated,
                name: "Loopback0".into(),
                interface_type: InterfaceType::Loopback,
                loopback_type: LoopbackType::Vpnv4,
                vlan_id: 0,
                ip_net: "10.0.0.1/32".parse().unwrap(),
                node_segment_idx: 100,
                user_tunnel_endpoint: false,
            }),
            Interface::V2(InterfaceV2 {
                status: InterfaceStatus::Activated,
                name: "Ethernet1".into(),
                interface_type: InterfaceType::Physical,
                interface_cyoa: InterfaceCYOA::GREOverDIA,
                interface_dia: InterfaceDIA::DIA,
                loopback_type: LoopbackType::None,
                bandwidth: 10_000_000_000,
                cir: 5_000_000_000,
                mtu: 9000,
                routing_mode: RoutingMode::BGP,
                vlan_id: 100,
                ip_net: "172.16.0.1/30".parse().unwrap(),
                node_segment_idx: 200,
                user_tunnel_endpoint: true,
            }),
        ],
        reference_count: 12,
        users_count: 5,
        max_users: 100,
        device_health: DeviceHealth::ReadyForUsers,
        desired_status: DeviceDesiredStatus::Activated,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "Device".into(),
        account_type: 5,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "5".into(), typ: "u8".into() },
            FieldValue { name: "Owner".into(), value: pubkey_bs58(&owner), typ: "pubkey".into() },
            FieldValue { name: "Index".into(), value: "7".into(), typ: "u128".into() },
            FieldValue { name: "BumpSeed".into(), value: "250".into(), typ: "u8".into() },
            FieldValue { name: "LocationPk".into(), value: pubkey_bs58(&location_pk), typ: "pubkey".into() },
            FieldValue { name: "ExchangePk".into(), value: pubkey_bs58(&exchange_pk), typ: "pubkey".into() },
            FieldValue { name: "DeviceType".into(), value: "2".into(), typ: "u8".into() },
            FieldValue { name: "PublicIp".into(), value: "203.0.113.1".into(), typ: "ipv4".into() },
            FieldValue { name: "Status".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Code".into(), value: "dz1".into(), typ: "string".into() },
            FieldValue { name: "DzPrefixesLen".into(), value: "1".into(), typ: "u32".into() },
            FieldValue { name: "DzPrefixes0".into(), value: "10.10.0.0/24".into(), typ: "networkv4".into() },
            FieldValue { name: "MetricsPublisherPk".into(), value: pubkey_bs58(&metrics_publisher_pk), typ: "pubkey".into() },
            FieldValue { name: "ContributorPk".into(), value: pubkey_bs58(&contributor_pk), typ: "pubkey".into() },
            FieldValue { name: "MgmtVrf".into(), value: "mgmt".into(), typ: "string".into() },
            FieldValue { name: "InterfacesLen".into(), value: "2".into(), typ: "u32".into() },
            // Interface 0 - V1
            FieldValue { name: "Interface0Version".into(), value: "0".into(), typ: "u8".into() },
            FieldValue { name: "Interface0Status".into(), value: "3".into(), typ: "u8".into() },
            FieldValue { name: "Interface0Name".into(), value: "Loopback0".into(), typ: "string".into() },
            FieldValue { name: "Interface0InterfaceType".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Interface0LoopbackType".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Interface0VlanId".into(), value: "0".into(), typ: "u16".into() },
            FieldValue { name: "Interface0IpNet".into(), value: "10.0.0.1/32".into(), typ: "networkv4".into() },
            FieldValue { name: "Interface0NodeSegmentIdx".into(), value: "100".into(), typ: "u16".into() },
            FieldValue { name: "Interface0UserTunnelEndpoint".into(), value: "false".into(), typ: "bool".into() },
            // Interface 1 - V2
            FieldValue { name: "Interface1Version".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Interface1Status".into(), value: "3".into(), typ: "u8".into() },
            FieldValue { name: "Interface1Name".into(), value: "Ethernet1".into(), typ: "string".into() },
            FieldValue { name: "Interface1InterfaceType".into(), value: "2".into(), typ: "u8".into() },
            FieldValue { name: "Interface1InterfaceCYOA".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Interface1InterfaceDIA".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Interface1LoopbackType".into(), value: "0".into(), typ: "u8".into() },
            FieldValue { name: "Interface1Bandwidth".into(), value: "10000000000".into(), typ: "u64".into() },
            FieldValue { name: "Interface1Cir".into(), value: "5000000000".into(), typ: "u64".into() },
            FieldValue { name: "Interface1Mtu".into(), value: "9000".into(), typ: "u16".into() },
            FieldValue { name: "Interface1RoutingMode".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Interface1VlanId".into(), value: "100".into(), typ: "u16".into() },
            FieldValue { name: "Interface1IpNet".into(), value: "172.16.0.1/30".into(), typ: "networkv4".into() },
            FieldValue { name: "Interface1NodeSegmentIdx".into(), value: "200".into(), typ: "u16".into() },
            FieldValue { name: "Interface1UserTunnelEndpoint".into(), value: "true".into(), typ: "bool".into() },
            FieldValue { name: "ReferenceCount".into(), value: "12".into(), typ: "u32".into() },
            FieldValue { name: "UsersCount".into(), value: "5".into(), typ: "u16".into() },
            FieldValue { name: "MaxUsers".into(), value: "100".into(), typ: "u16".into() },
            FieldValue { name: "DeviceHealth".into(), value: "3".into(), typ: "u8".into() },
            FieldValue { name: "DesiredStatus".into(), value: "1".into(), typ: "u8".into() },
        ],
    };

    write_fixture(dir, "device", &data, &meta);
}

fn generate_link(dir: &Path) {
    let owner = pubkey_from_byte(0x50);
    let side_a_pk = pubkey_from_byte(0x51);
    let side_z_pk = pubkey_from_byte(0x52);
    let contributor_pk = pubkey_from_byte(0x53);

    let val = Link {
        account_type: AccountType::Link,
        owner,
        index: 99,
        bump_seed: 249,
        side_a_pk,
        side_z_pk,
        link_type: LinkLinkType::WAN,
        bandwidth: 10_000_000_000,
        mtu: 9000,
        delay_ns: 5_000_000,
        jitter_ns: 100_000,
        tunnel_id: 500,
        tunnel_net: "169.254.1.0/30".parse().unwrap(),
        status: LinkStatus::Activated,
        code: "ams-fra".into(),
        contributor_pk,
        side_a_iface_name: "Ethernet2".into(),
        side_z_iface_name: "Ethernet2".into(),
        delay_override_ns: 0,
        link_health: LinkHealth::ReadyForService,
        desired_status: LinkDesiredStatus::Activated,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "Link".into(),
        account_type: 6,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "6".into(), typ: "u8".into() },
            FieldValue { name: "Owner".into(), value: pubkey_bs58(&owner), typ: "pubkey".into() },
            FieldValue { name: "Index".into(), value: "99".into(), typ: "u128".into() },
            FieldValue { name: "BumpSeed".into(), value: "249".into(), typ: "u8".into() },
            FieldValue { name: "SideAPk".into(), value: pubkey_bs58(&side_a_pk), typ: "pubkey".into() },
            FieldValue { name: "SideZPk".into(), value: pubkey_bs58(&side_z_pk), typ: "pubkey".into() },
            FieldValue { name: "LinkType".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Bandwidth".into(), value: "10000000000".into(), typ: "u64".into() },
            FieldValue { name: "Mtu".into(), value: "9000".into(), typ: "u32".into() },
            FieldValue { name: "DelayNs".into(), value: "5000000".into(), typ: "u64".into() },
            FieldValue { name: "JitterNs".into(), value: "100000".into(), typ: "u64".into() },
            FieldValue { name: "TunnelId".into(), value: "500".into(), typ: "u16".into() },
            FieldValue { name: "TunnelNet".into(), value: "169.254.1.0/30".into(), typ: "networkv4".into() },
            FieldValue { name: "Status".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Code".into(), value: "ams-fra".into(), typ: "string".into() },
            FieldValue { name: "ContributorPk".into(), value: pubkey_bs58(&contributor_pk), typ: "pubkey".into() },
            FieldValue { name: "SideAIfaceName".into(), value: "Ethernet2".into(), typ: "string".into() },
            FieldValue { name: "SideZIfaceName".into(), value: "Ethernet2".into(), typ: "string".into() },
            FieldValue { name: "DelayOverrideNs".into(), value: "0".into(), typ: "u64".into() },
            FieldValue { name: "LinkHealth".into(), value: "2".into(), typ: "u8".into() },
            FieldValue { name: "DesiredStatus".into(), value: "1".into(), typ: "u8".into() },
        ],
    };

    write_fixture(dir, "link", &data, &meta);
}

fn generate_user(dir: &Path) {
    let owner = pubkey_from_byte(0x60);
    let tenant_pk = pubkey_from_byte(0x61);
    let device_pk = pubkey_from_byte(0x62);
    let publisher_pk = pubkey_from_byte(0x63);
    let subscriber_pk = pubkey_from_byte(0x64);
    let validator_pubkey = pubkey_from_byte(0x65);

    let val = User {
        account_type: AccountType::User,
        owner,
        index: 200,
        bump_seed: 248,
        user_type: UserType::Multicast,
        tenant_pk,
        device_pk,
        cyoa_type: UserCYOA::GREOverFabric,
        client_ip: Ipv4Addr::new(198, 51, 100, 10),
        dz_ip: Ipv4Addr::new(10, 200, 0, 1),
        tunnel_id: 100,
        tunnel_net: "169.254.100.0/30".parse().unwrap(),
        status: UserStatus::Activated,
        publishers: vec![publisher_pk],
        subscribers: vec![subscriber_pk],
        validator_pubkey,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "User".into(),
        account_type: 7,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "7".into(), typ: "u8".into() },
            FieldValue { name: "Owner".into(), value: pubkey_bs58(&owner), typ: "pubkey".into() },
            FieldValue { name: "Index".into(), value: "200".into(), typ: "u128".into() },
            FieldValue { name: "BumpSeed".into(), value: "248".into(), typ: "u8".into() },
            FieldValue { name: "UserType".into(), value: "3".into(), typ: "u8".into() },
            FieldValue { name: "TenantPk".into(), value: pubkey_bs58(&tenant_pk), typ: "pubkey".into() },
            FieldValue { name: "DevicePk".into(), value: pubkey_bs58(&device_pk), typ: "pubkey".into() },
            FieldValue { name: "CyoaType".into(), value: "2".into(), typ: "u8".into() },
            FieldValue { name: "ClientIp".into(), value: "198.51.100.10".into(), typ: "ipv4".into() },
            FieldValue { name: "DzIp".into(), value: "10.200.0.1".into(), typ: "ipv4".into() },
            FieldValue { name: "TunnelId".into(), value: "100".into(), typ: "u16".into() },
            FieldValue { name: "TunnelNet".into(), value: "169.254.100.0/30".into(), typ: "networkv4".into() },
            FieldValue { name: "Status".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "PublishersLen".into(), value: "1".into(), typ: "u32".into() },
            FieldValue { name: "Publishers0".into(), value: pubkey_bs58(&publisher_pk), typ: "pubkey".into() },
            FieldValue { name: "SubscribersLen".into(), value: "1".into(), typ: "u32".into() },
            FieldValue { name: "Subscribers0".into(), value: pubkey_bs58(&subscriber_pk), typ: "pubkey".into() },
            FieldValue { name: "ValidatorPubkey".into(), value: pubkey_bs58(&validator_pubkey), typ: "pubkey".into() },
        ],
    };

    write_fixture(dir, "user", &data, &meta);
}

fn generate_multicast_group(dir: &Path) {
    let owner = pubkey_from_byte(0x70);
    let tenant_pk = pubkey_from_byte(0x71);

    let val = MulticastGroup {
        account_type: AccountType::MulticastGroup,
        owner,
        index: 30,
        bump_seed: 247,
        tenant_pk,
        multicast_ip: Ipv4Addr::new(239, 1, 1, 1),
        max_bandwidth: 1_000_000_000,
        status: MulticastGroupStatus::Activated,
        code: "demo".into(),
        publisher_count: 2,
        subscriber_count: 10,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "MulticastGroup".into(),
        account_type: 8,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "8".into(), typ: "u8".into() },
            FieldValue { name: "Owner".into(), value: pubkey_bs58(&owner), typ: "pubkey".into() },
            FieldValue { name: "Index".into(), value: "30".into(), typ: "u128".into() },
            FieldValue { name: "BumpSeed".into(), value: "247".into(), typ: "u8".into() },
            FieldValue { name: "TenantPk".into(), value: pubkey_bs58(&tenant_pk), typ: "pubkey".into() },
            FieldValue { name: "MulticastIp".into(), value: "239.1.1.1".into(), typ: "ipv4".into() },
            FieldValue { name: "MaxBandwidth".into(), value: "1000000000".into(), typ: "u64".into() },
            FieldValue { name: "Status".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Code".into(), value: "demo".into(), typ: "string".into() },
            FieldValue { name: "PublisherCount".into(), value: "2".into(), typ: "u32".into() },
            FieldValue { name: "SubscriberCount".into(), value: "10".into(), typ: "u32".into() },
        ],
    };

    write_fixture(dir, "multicast_group", &data, &meta);
}

fn generate_program_config(dir: &Path) {
    let val = ProgramConfig {
        account_type: AccountType::ProgramConfig,
        bump_seed: 246,
        version: ProgramVersion {
            major: 1,
            minor: 2,
            patch: 3,
        },
        min_compatible_version: ProgramVersion {
            major: 1,
            minor: 0,
            patch: 0,
        },
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "ProgramConfig".into(),
        account_type: 9,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "9".into(), typ: "u8".into() },
            FieldValue { name: "BumpSeed".into(), value: "246".into(), typ: "u8".into() },
            FieldValue { name: "VersionMajor".into(), value: "1".into(), typ: "u32".into() },
            FieldValue { name: "VersionMinor".into(), value: "2".into(), typ: "u32".into() },
            FieldValue { name: "VersionPatch".into(), value: "3".into(), typ: "u32".into() },
            FieldValue { name: "MinCompatibleVersionMajor".into(), value: "1".into(), typ: "u32".into() },
            FieldValue { name: "MinCompatibleVersionMinor".into(), value: "0".into(), typ: "u32".into() },
            FieldValue { name: "MinCompatibleVersionPatch".into(), value: "0".into(), typ: "u32".into() },
        ],
    };

    write_fixture(dir, "program_config", &data, &meta);
}

fn generate_contributor(dir: &Path) {
    let owner = pubkey_from_byte(0x80);
    let ops_manager_pk = pubkey_from_byte(0x81);

    let val = Contributor {
        account_type: AccountType::Contributor,
        owner,
        index: 550,
        bump_seed: 245,
        status: ContributorStatus::Activated,
        code: "co01".into(),
        reference_count: 7,
        ops_manager_pk,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "Contributor".into(),
        account_type: 10,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "10".into(), typ: "u8".into() },
            FieldValue { name: "Owner".into(), value: pubkey_bs58(&owner), typ: "pubkey".into() },
            FieldValue { name: "Index".into(), value: "550".into(), typ: "u128".into() },
            FieldValue { name: "BumpSeed".into(), value: "245".into(), typ: "u8".into() },
            FieldValue { name: "Status".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "Code".into(), value: "co01".into(), typ: "string".into() },
            FieldValue { name: "ReferenceCount".into(), value: "7".into(), typ: "u32".into() },
            FieldValue { name: "OpsManagerPk".into(), value: pubkey_bs58(&ops_manager_pk), typ: "pubkey".into() },
        ],
    };

    write_fixture(dir, "contributor", &data, &meta);
}

fn generate_access_pass(dir: &Path) {
    let owner = pubkey_from_byte(0x90);
    let user_payer = pubkey_from_byte(0x91);

    let val = AccessPass {
        account_type: AccountType::AccessPass,
        owner,
        bump_seed: 244,
        accesspass_type: AccessPassType::Prepaid,
        client_ip: Ipv4Addr::new(198, 51, 100, 20),
        user_payer,
        last_access_epoch: u64::MAX,
        connection_count: 3,
        status: AccessPassStatus::Connected,
        mgroup_pub_allowlist: vec![],
        mgroup_sub_allowlist: vec![],
        flags: 0x01,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "AccessPass".into(),
        account_type: 11,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "11".into(), typ: "u8".into() },
            FieldValue { name: "Owner".into(), value: pubkey_bs58(&owner), typ: "pubkey".into() },
            FieldValue { name: "BumpSeed".into(), value: "244".into(), typ: "u8".into() },
            FieldValue { name: "AccessPassType".into(), value: "0".into(), typ: "u8".into() },
            FieldValue { name: "ClientIp".into(), value: "198.51.100.20".into(), typ: "ipv4".into() },
            FieldValue { name: "UserPayer".into(), value: pubkey_bs58(&user_payer), typ: "pubkey".into() },
            FieldValue { name: "LastAccessEpoch".into(), value: "18446744073709551615".into(), typ: "u64".into() },
            FieldValue { name: "ConnectionCount".into(), value: "3".into(), typ: "u16".into() },
            FieldValue { name: "Status".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "MgroupPubAllowlistLen".into(), value: "0".into(), typ: "u32".into() },
            FieldValue { name: "MgroupSubAllowlistLen".into(), value: "0".into(), typ: "u32".into() },
            FieldValue { name: "Flags".into(), value: "1".into(), typ: "u8".into() },
        ],
    };

    write_fixture(dir, "access_pass", &data, &meta);
}

fn generate_access_pass_validator(dir: &Path) {
    let owner = pubkey_from_byte(0xA0);
    let user_payer = pubkey_from_byte(0xA1);
    let validator_pk = pubkey_from_byte(0xA2);
    let mgroup_pub = pubkey_from_byte(0xA3);
    let mgroup_sub = pubkey_from_byte(0xA4);

    let val = AccessPass {
        account_type: AccountType::AccessPass,
        owner,
        bump_seed: 243,
        accesspass_type: AccessPassType::SolanaValidator(validator_pk),
        client_ip: Ipv4Addr::new(10, 0, 0, 50),
        user_payer,
        last_access_epoch: 1000,
        connection_count: 1,
        status: AccessPassStatus::Connected,
        mgroup_pub_allowlist: vec![mgroup_pub],
        mgroup_sub_allowlist: vec![mgroup_sub],
        flags: 0x03,
    };

    let data = borsh::to_vec(&val).unwrap();

    let meta = FixtureMeta {
        name: "AccessPassValidator".into(),
        account_type: 11,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "11".into(), typ: "u8".into() },
            FieldValue { name: "Owner".into(), value: pubkey_bs58(&owner), typ: "pubkey".into() },
            FieldValue { name: "BumpSeed".into(), value: "243".into(), typ: "u8".into() },
            FieldValue { name: "AccessPassType".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "AccessPassTypeValidatorPubkey".into(), value: pubkey_bs58(&validator_pk), typ: "pubkey".into() },
            FieldValue { name: "ClientIp".into(), value: "10.0.0.50".into(), typ: "ipv4".into() },
            FieldValue { name: "UserPayer".into(), value: pubkey_bs58(&user_payer), typ: "pubkey".into() },
            FieldValue { name: "LastAccessEpoch".into(), value: "1000".into(), typ: "u64".into() },
            FieldValue { name: "ConnectionCount".into(), value: "1".into(), typ: "u16".into() },
            FieldValue { name: "Status".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "MgroupPubAllowlistLen".into(), value: "1".into(), typ: "u32".into() },
            FieldValue { name: "MgroupPubAllowlist0".into(), value: pubkey_bs58(&mgroup_pub), typ: "pubkey".into() },
            FieldValue { name: "MgroupSubAllowlistLen".into(), value: "1".into(), typ: "u32".into() },
            FieldValue { name: "MgroupSubAllowlist0".into(), value: pubkey_bs58(&mgroup_sub), typ: "pubkey".into() },
            FieldValue { name: "Flags".into(), value: "3".into(), typ: "u8".into() },
        ],
    };

    write_fixture(dir, "access_pass_validator", &data, &meta);
}

/// ResourceExtension uses a fixed binary layout with bitmap at offset 88,
/// so we manually construct the bytes rather than using borsh::to_vec.
const RESOURCE_EXTENSION_BITMAP_OFFSET: usize = 88;

fn generate_resource_extension_id(dir: &Path) {
    let owner = pubkey_from_byte(0xB0);
    let associated_with = pubkey_from_byte(0xB1);
    let range_start: u16 = 0;
    let range_end: u16 = 64;
    let first_free_index: u64 = 5; // Simulates 5 allocations made

    // Calculate bitmap size: (64 - 0) / 8 = 8 bytes, rounded up to multiple of 8 = 8 bytes
    let bitmap_size = IdAllocator::bitmap_required_size((range_start, range_end));
    let total_size = RESOURCE_EXTENSION_BITMAP_OFFSET + bitmap_size;
    let mut data = vec![0u8; total_size];

    // Manually serialize the header
    let mut offset = 0;

    // [0] account_type = 12 (ResourceExtension)
    data[offset] = 12;
    offset += 1;

    // [1-32] owner pubkey
    data[offset..offset + 32].copy_from_slice(&owner.to_bytes());
    offset += 32;

    // [33] bump_seed
    data[offset] = 242;
    offset += 1;

    // [34-65] associated_with pubkey
    data[offset..offset + 32].copy_from_slice(&associated_with.to_bytes());
    offset += 32;

    // Serialize the Allocator enum: discriminant + IdAllocator
    // discriminant = 1 (Id)
    data[offset] = 1;
    offset += 1;

    // IdAllocator: range (u16, u16) + first_free_index (u64)
    data[offset..offset + 2].copy_from_slice(&range_start.to_le_bytes());
    offset += 2;
    data[offset..offset + 2].copy_from_slice(&range_end.to_le_bytes());
    offset += 2;
    data[offset..offset + 8].copy_from_slice(&first_free_index.to_le_bytes());
    // offset += 8; // Not needed, padding follows

    // Bitmap at offset 88: set bits 0-4 as allocated (0x1F = 0b00011111)
    data[RESOURCE_EXTENSION_BITMAP_OFFSET] = 0x1F;

    let meta = FixtureMeta {
        name: "ResourceExtensionId".into(),
        account_type: 12,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "12".into(), typ: "u8".into() },
            FieldValue { name: "Owner".into(), value: pubkey_bs58(&owner), typ: "pubkey".into() },
            FieldValue { name: "BumpSeed".into(), value: "242".into(), typ: "u8".into() },
            FieldValue { name: "AssociatedWith".into(), value: pubkey_bs58(&associated_with), typ: "pubkey".into() },
            FieldValue { name: "AllocatorType".into(), value: "1".into(), typ: "u8".into() },
            FieldValue { name: "RangeStart".into(), value: "0".into(), typ: "u16".into() },
            FieldValue { name: "RangeEnd".into(), value: "64".into(), typ: "u16".into() },
            FieldValue { name: "FirstFreeIndex".into(), value: "5".into(), typ: "u64".into() },
            FieldValue { name: "TotalCapacity".into(), value: "64".into(), typ: "u64".into() },
            FieldValue { name: "AllocatedCount".into(), value: "5".into(), typ: "u64".into() },
            FieldValue { name: "AvailableCount".into(), value: "59".into(), typ: "u64".into() },
        ],
    };

    write_fixture(dir, "resource_extension_id", &data, &meta);
}

fn generate_resource_extension_ip(dir: &Path) {
    let owner = pubkey_from_byte(0xC0);
    let associated_with = pubkey_from_byte(0xC1);
    let base_net: std::net::Ipv4Addr = "10.100.0.0".parse().unwrap();
    let prefix: u8 = 24; // /24 = 256 addresses
    let first_free_index: u64 = 4; // Simulates 4 allocations made

    // Calculate bitmap size for /24: 256 addresses / 8 = 32 bytes, rounded to multiple of 8 = 32 bytes
    let bitmap_size = IpAllocator::bitmap_required_size(prefix);
    let total_size = RESOURCE_EXTENSION_BITMAP_OFFSET + bitmap_size;
    let mut data = vec![0u8; total_size];

    // Manually serialize the header
    let mut offset = 0;

    // [0] account_type = 12 (ResourceExtension)
    data[offset] = 12;
    offset += 1;

    // [1-32] owner pubkey
    data[offset..offset + 32].copy_from_slice(&owner.to_bytes());
    offset += 32;

    // [33] bump_seed
    data[offset] = 241;
    offset += 1;

    // [34-65] associated_with pubkey
    data[offset..offset + 32].copy_from_slice(&associated_with.to_bytes());
    offset += 32;

    // Serialize the Allocator enum: discriminant + IpAllocator
    // discriminant = 0 (Ip)
    data[offset] = 0;
    offset += 1;

    // IpAllocator: base_net (4 bytes IP + 1 byte prefix) + first_free_index (u64)
    data[offset..offset + 4].copy_from_slice(&base_net.octets());
    offset += 4;
    data[offset] = prefix;
    offset += 1;
    data[offset..offset + 8].copy_from_slice(&first_free_index.to_le_bytes());
    // offset += 8; // Not needed, padding follows

    // Bitmap at offset 88: set bits 0-3 as allocated (0x0F = 0b00001111)
    data[RESOURCE_EXTENSION_BITMAP_OFFSET] = 0x0F;

    let meta = FixtureMeta {
        name: "ResourceExtensionIp".into(),
        account_type: 12,
        fields: vec![
            FieldValue { name: "AccountType".into(), value: "12".into(), typ: "u8".into() },
            FieldValue { name: "Owner".into(), value: pubkey_bs58(&owner), typ: "pubkey".into() },
            FieldValue { name: "BumpSeed".into(), value: "241".into(), typ: "u8".into() },
            FieldValue { name: "AssociatedWith".into(), value: pubkey_bs58(&associated_with), typ: "pubkey".into() },
            FieldValue { name: "AllocatorType".into(), value: "0".into(), typ: "u8".into() },
            FieldValue { name: "BaseNet".into(), value: "10.100.0.0/24".into(), typ: "networkv4".into() },
            FieldValue { name: "FirstFreeIndex".into(), value: "4".into(), typ: "u64".into() },
            FieldValue { name: "TotalCapacity".into(), value: "256".into(), typ: "u64".into() },
            FieldValue { name: "AllocatedCount".into(), value: "4".into(), typ: "u64".into() },
            FieldValue { name: "AvailableCount".into(), value: "252".into(), typ: "u64".into() },
        ],
    };

    write_fixture(dir, "resource_extension_ip", &data, &meta);
}
