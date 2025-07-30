use doublezero_serviceability::state::{
    device::Device, exchange::Exchange, link::Link, location::Location,
    multicastgroup::MulticastGroup, user::User,
};
use doublezero_telemetry::state::device_latency_samples::DeviceLatencySamples;
use serde::{Deserialize, Serialize};
use solana_sdk::pubkey::Pubkey;

/// DB representation of a Location
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DZLocation {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
    pub index: u128,
    pub bump_seed: u8,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: u32,
    pub status: String,
    pub code: String,
    pub name: String,
    pub country: String,
}

impl DZLocation {
    pub fn from_solana(pubkey: Pubkey, location: &Location) -> Self {
        Self {
            pubkey,
            owner: location.owner,
            index: location.index,
            bump_seed: location.bump_seed,
            lat: location.lat,
            lng: location.lng,
            loc_id: location.loc_id,
            status: location.status.to_string(),
            code: location.code.clone(),
            name: location.name.clone(),
            country: location.country.clone(),
        }
    }
}

/// DB representation of an Exchange
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DZExchange {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
    pub index: u128,
    pub bump_seed: u8,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: u32,
    pub status: String,
    pub code: String,
    pub name: String,
}

impl DZExchange {
    pub fn from_solana(pubkey: Pubkey, exchange: &Exchange) -> Self {
        Self {
            pubkey,
            owner: exchange.owner,
            index: exchange.index,
            bump_seed: exchange.bump_seed,
            lat: exchange.lat,
            lng: exchange.lng,
            loc_id: exchange.loc_id,
            status: exchange.status.to_string(),
            code: exchange.code.clone(),
            name: exchange.name.clone(),
        }
    }
}

/// DB representation of a Device
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DZDevice {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
    pub index: u128,
    pub bump_seed: u8,
    pub location_pubkey: Option<Pubkey>,
    pub exchange_pubkey: Option<Pubkey>,
    pub device_type: String,
    pub public_ip: String,
    pub status: String,
    pub code: String,
    pub dz_prefixes: Vec<String>,
    pub metrics_publisher_pk: Pubkey,
}

impl DZDevice {
    pub fn from_solana(pubkey: Pubkey, device: &Device) -> Self {
        Self {
            pubkey,
            owner: device.owner,
            index: device.index,
            bump_seed: device.bump_seed,
            location_pubkey: if device.location_pk != Pubkey::default() {
                Some(device.location_pk)
            } else {
                None
            },
            exchange_pubkey: if device.exchange_pk != Pubkey::default() {
                Some(device.exchange_pk)
            } else {
                None
            },
            device_type: device.device_type.to_string(),
            public_ip: device.public_ip.to_string(),
            status: device.status.to_string(),
            code: device.code.clone(),
            dz_prefixes: device
                .dz_prefixes
                .iter()
                .map(|net| format!("{}/{}", net.ip(), net.prefix()))
                .collect(),
            metrics_publisher_pk: device.metrics_publisher_pk,
        }
    }
}

/// DB representation of a Link
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DZLink {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
    pub index: u128,
    pub bump_seed: u8,
    pub from_device_pubkey: Option<Pubkey>,
    pub to_device_pubkey: Option<Pubkey>,
    pub link_type: String,
    pub bandwidth: u64,
    pub mtu: u32,
    pub delay_ns: u64,
    pub jitter_ns: u64,
    pub tunnel_id: u16,
    pub tunnel_net: String,
    pub status: String,
    pub code: String,
}

impl DZLink {
    pub fn from_solana(pubkey: Pubkey, link: &Link) -> Self {
        Self {
            pubkey,
            owner: link.owner,
            index: link.index,
            bump_seed: link.bump_seed,
            from_device_pubkey: if link.side_a_pk != Pubkey::default() {
                Some(link.side_a_pk)
            } else {
                None
            },
            to_device_pubkey: if link.side_z_pk != Pubkey::default() {
                Some(link.side_z_pk)
            } else {
                None
            },
            link_type: link.link_type.to_string(),
            bandwidth: link.bandwidth,
            mtu: link.mtu,
            delay_ns: link.delay_ns,
            jitter_ns: link.jitter_ns,
            tunnel_id: link.tunnel_id,
            tunnel_net: format!("{}/{}", link.tunnel_net.ip(), link.tunnel_net.prefix()),
            status: link.status.to_string(),
            code: link.code.clone(),
        }
    }
}

/// DB representation of a User
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DZUser {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
    pub index: u128,
    pub bump_seed: u8,
    pub user_type: String,
    pub tenant_pk: Pubkey,
    pub device_pk: Option<Pubkey>,
    pub cyoa_type: String,
    pub client_ip: String,
    pub dz_ip: String,
    pub tunnel_id: u16,
    pub tunnel_net: String,
    pub status: String,
    pub publishers: Vec<String>,
    pub subscribers: Vec<String>,
}

impl DZUser {
    pub fn from_solana(pubkey: Pubkey, user: &User) -> Self {
        Self {
            pubkey,
            owner: user.owner,
            index: user.index,
            bump_seed: user.bump_seed,
            user_type: user.user_type.to_string(),
            tenant_pk: user.tenant_pk,
            device_pk: if user.device_pk != Pubkey::default() {
                Some(user.device_pk)
            } else {
                None
            },
            cyoa_type: user.cyoa_type.to_string(),
            client_ip: user.client_ip.to_string(),
            dz_ip: user.dz_ip.to_string(),
            tunnel_id: user.tunnel_id,
            tunnel_net: format!("{}/{}", user.tunnel_net.ip(), user.tunnel_net.prefix()),
            status: user.status.to_string(),
            publishers: user.publishers.iter().map(|p| p.to_string()).collect(),
            subscribers: user.subscribers.iter().map(|s| s.to_string()).collect(),
        }
    }
}

/// DB representation of a Multicast Group
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DZMulticastGroup {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
    pub index: u128,
    pub bump_seed: u8,
    pub tenant_pk: Pubkey,
    pub multicast_ip: String,
    pub max_bandwidth: u64,
    pub status: String,
    pub code: String,
    pub pub_allowlist: Vec<String>,
    pub sub_allowlist: Vec<String>,
    pub publishers: Vec<String>,
    pub subscribers: Vec<String>,
}

impl DZMulticastGroup {
    pub fn from_solana(pubkey: Pubkey, group: &MulticastGroup) -> Self {
        Self {
            pubkey,
            owner: group.owner,
            index: group.index,
            bump_seed: group.bump_seed,
            tenant_pk: group.tenant_pk,
            multicast_ip: group.multicast_ip.to_string(),
            max_bandwidth: group.max_bandwidth,
            status: group.status.to_string(),
            code: group.code.clone(),
            pub_allowlist: group.pub_allowlist.iter().map(|p| p.to_string()).collect(),
            sub_allowlist: group.sub_allowlist.iter().map(|s| s.to_string()).collect(),
            publishers: group.publishers.iter().map(|p| p.to_string()).collect(),
            subscribers: group.subscribers.iter().map(|s| s.to_string()).collect(),
        }
    }
}

/// Struct for all network data
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct DZServiceabilityData {
    pub locations: Vec<DZLocation>,
    pub exchanges: Vec<DZExchange>,
    pub devices: Vec<DZDevice>,
    pub links: Vec<DZLink>,
    pub users: Vec<DZUser>,
    pub multicast_groups: Vec<DZMulticastGroup>,
}

/// DB representation of DeviceLatencySamples
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DbDeviceLatencySamples {
    pub pubkey: Pubkey,
    pub epoch: u64,
    pub origin_device_pk: Pubkey,
    pub target_device_pk: Pubkey,
    pub link_pk: Pubkey,
    pub origin_device_location_pk: Pubkey,
    pub target_device_location_pk: Pubkey,
    pub origin_device_agent_pk: Pubkey,
    pub sampling_interval_us: u64,
    pub start_timestamp_us: u64,
    pub samples: Vec<u32>,
    pub sample_count: u32,
}

impl DbDeviceLatencySamples {
    pub fn from_solana(pubkey: Pubkey, samples: &DeviceLatencySamples) -> Self {
        Self {
            pubkey,
            epoch: samples.header.epoch,
            origin_device_pk: samples.header.origin_device_pk,
            target_device_pk: samples.header.target_device_pk,
            link_pk: samples.header.link_pk,
            origin_device_location_pk: samples.header.origin_device_location_pk,
            target_device_location_pk: samples.header.target_device_location_pk,
            origin_device_agent_pk: samples.header.origin_device_agent_pk,
            sampling_interval_us: samples.header.sampling_interval_microseconds,
            start_timestamp_us: samples.header.start_timestamp_microseconds,
            samples: samples.samples.clone(),
            sample_count: samples.header.next_sample_index,
        }
    }
}

/// Telemetry data container
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct DZDTelemetryData {
    pub device_latency_samples: Vec<DbDeviceLatencySamples>,
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_telemetry::state::device_latency_samples::DeviceLatencySamplesHeader;
    use solana_sdk::pubkey::Pubkey;
    use std::net::Ipv4Addr;

    // Helper function to create test pubkeys
    fn test_pubkey(seed: u8) -> Pubkey {
        Pubkey::new_from_array([seed; 32])
    }

    #[test]
    fn test_ipv4_to_string() {
        assert_eq!(Ipv4Addr::new(192, 168, 1, 1).to_string(), "192.168.1.1");
        assert_eq!(Ipv4Addr::new(10, 0, 0, 1).to_string(), "10.0.0.1");
        assert_eq!(
            Ipv4Addr::new(255, 255, 255, 255).to_string(),
            "255.255.255.255"
        );
    }

    #[test]
    fn test_db_device_latency_samples_from_solana() {
        let pubkey = test_pubkey(1);
        let samples = DeviceLatencySamples {
            header: DeviceLatencySamplesHeader {
                account_type:
                    doublezero_telemetry::state::accounttype::AccountType::DeviceLatencySamples,
                epoch: 100,
                origin_device_agent_pk: test_pubkey(2),
                origin_device_pk: test_pubkey(3),
                target_device_pk: test_pubkey(4),
                origin_device_location_pk: test_pubkey(5),
                target_device_location_pk: test_pubkey(6),
                link_pk: test_pubkey(7),
                sampling_interval_microseconds: 5_000_000,
                start_timestamp_microseconds: 1_700_000_000_000_000,
                next_sample_index: 3,
                _unused: [0; 128],
            },
            samples: vec![100, 200, 300],
        };

        let db_samples = DbDeviceLatencySamples::from_solana(pubkey, &samples);

        assert_eq!(db_samples.pubkey, pubkey);
        assert_eq!(db_samples.epoch, 100);
        assert_eq!(db_samples.origin_device_pk, test_pubkey(3));
        assert_eq!(db_samples.target_device_pk, test_pubkey(4));
        assert_eq!(db_samples.link_pk, test_pubkey(7));
        assert_eq!(db_samples.origin_device_location_pk, test_pubkey(5));
        assert_eq!(db_samples.target_device_location_pk, test_pubkey(6));
        assert_eq!(db_samples.origin_device_agent_pk, test_pubkey(2));
        assert_eq!(db_samples.sampling_interval_us, 5_000_000);
        assert_eq!(db_samples.start_timestamp_us, 1_700_000_000_000_000);
        assert_eq!(db_samples.samples, vec![100, 200, 300]);
        assert_eq!(db_samples.sample_count, 3);
    }
}
