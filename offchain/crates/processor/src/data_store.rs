use chrono::{DateTime, Utc};
use ingestor::fetcher::FetchData;
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};
use std::{collections::HashMap, convert::TryInto};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DataStore {
    pub devices: HashMap<String, Device>,
    pub locations: HashMap<String, Location>,
    pub exchanges: HashMap<String, Exchange>,
    pub links: HashMap<String, Link>,
    pub users: HashMap<String, User>,
    pub multicast_groups: HashMap<String, MulticastGroup>,
    pub telemetry_samples: Vec<TelemetrySample>,
    pub demand_matrix: Vec<DemandEntry>,
    pub metadata: FetchMetadata,
}

impl TryFrom<FetchData> for DataStore {
    type Error = anyhow::Error;

    fn try_from(fetch_data: FetchData) -> std::result::Result<Self, Self::Error> {
        let mut data_store = DataStore::new(fetch_data.after_us, fetch_data.before_us);

        // Convert locations
        for dz_loc in fetch_data.dz_serviceability.locations {
            let location = Location {
                pubkey: dz_loc.pubkey.to_string(),
                owner: dz_loc.owner.to_string(),
                index: dz_loc.index.try_into().map_err(|_| {
                    anyhow::anyhow!("Location index {} too large for u64", dz_loc.index)
                })?,
                bump_seed: dz_loc.bump_seed,
                lat: dz_loc.lat,
                lng: dz_loc.lng,
                loc_id: dz_loc.loc_id,
                status: dz_loc.status,
                code: dz_loc.code,
                name: dz_loc.name,
                country: dz_loc.country,
            };
            data_store
                .locations
                .insert(location.pubkey.clone(), location);
        }

        // Convert exchanges
        for dz_ex in fetch_data.dz_serviceability.exchanges {
            let exchange = Exchange {
                pubkey: dz_ex.pubkey.to_string(),
                owner: dz_ex.owner.to_string(),
                index: dz_ex.index.try_into().map_err(|_| {
                    anyhow::anyhow!("Exchange index {} too large for u64", dz_ex.index)
                })?,
                bump_seed: dz_ex.bump_seed,
                lat: dz_ex.lat,
                lng: dz_ex.lng,
                loc_id: dz_ex.loc_id,
                status: dz_ex.status,
                code: dz_ex.code,
                name: dz_ex.name,
            };
            data_store
                .exchanges
                .insert(exchange.pubkey.clone(), exchange);
        }

        // Convert devices
        for dz_dev in fetch_data.dz_serviceability.devices {
            let device = Device {
                pubkey: dz_dev.pubkey.to_string(),
                owner: dz_dev.owner.to_string(),
                index: dz_dev.index.try_into().map_err(|_| {
                    anyhow::anyhow!("Device index {} too large for u64", dz_dev.index)
                })?,
                bump_seed: dz_dev.bump_seed,
                location_pubkey: dz_dev.location_pubkey.map(|pk| pk.to_string()),
                exchange_pubkey: dz_dev.exchange_pubkey.map(|pk| pk.to_string()),
                device_type: dz_dev.device_type,
                public_ip: dz_dev.public_ip,
                status: dz_dev.status,
                code: dz_dev.code,
                dz_prefixes: dz_dev.dz_prefixes,
                metrics_publisher_pk: dz_dev.metrics_publisher_pk.to_string(),
            };
            data_store.devices.insert(device.pubkey.clone(), device);
        }

        // Convert links
        for dz_link in fetch_data.dz_serviceability.links {
            let link = Link {
                pubkey: dz_link.pubkey.to_string(),
                owner: dz_link.owner.to_string(),
                index: dz_link.index.try_into().map_err(|_| {
                    anyhow::anyhow!("Link index {} too large for u64", dz_link.index)
                })?,
                bump_seed: dz_link.bump_seed,
                from_device_pubkey: dz_link.from_device_pubkey.map(|pk| pk.to_string()),
                to_device_pubkey: dz_link.to_device_pubkey.map(|pk| pk.to_string()),
                link_type: dz_link.link_type,
                bandwidth: dz_link.bandwidth,
                mtu: dz_link.mtu,
                delay_ns: dz_link.delay_ns,
                jitter_ns: dz_link.jitter_ns,
                tunnel_id: dz_link.tunnel_id,
                tunnel_net: vec![dz_link.tunnel_net],
                status: dz_link.status,
                code: dz_link.code,
            };
            data_store.links.insert(link.pubkey.clone(), link);
        }

        // Convert users
        for dz_user in fetch_data.dz_serviceability.users {
            let user = User {
                pubkey: dz_user.pubkey.to_string(),
                owner: dz_user.owner.to_string(),
                index: dz_user.index.try_into().map_err(|_| {
                    anyhow::anyhow!("User index {} too large for u64", dz_user.index)
                })?,
                bump_seed: dz_user.bump_seed,
                user_type: dz_user.user_type,
                tenant_pk: dz_user.tenant_pk.to_string(),
                device_pk: dz_user.device_pk.map(|pk| pk.to_string()),
                cyoa_type: dz_user.cyoa_type,
                client_ip: dz_user.client_ip,
                dz_ip: dz_user.dz_ip,
                tunnel_id: dz_user.tunnel_id,
                tunnel_net: vec![dz_user.tunnel_net],
                status: dz_user.status,
                publishers: dz_user.publishers.iter().map(|pk| pk.to_string()).collect(),
                subscribers: dz_user
                    .subscribers
                    .iter()
                    .map(|pk| pk.to_string())
                    .collect(),
            };
            data_store.users.insert(user.pubkey.clone(), user);
        }

        // Convert multicast groups
        for dz_group in fetch_data.dz_serviceability.multicast_groups {
            let group = MulticastGroup {
                pubkey: dz_group.pubkey.to_string(),
                owner: dz_group.owner.to_string(),
                index: dz_group.index.try_into().map_err(|_| {
                    anyhow::anyhow!("MulticastGroup index {} too large for u64", dz_group.index)
                })?,
                bump_seed: dz_group.bump_seed,
                tenant_pk: dz_group.tenant_pk.to_string(),
                multicast_ip: dz_group.multicast_ip,
                max_bandwidth: dz_group.max_bandwidth,
                status: dz_group.status,
                code: dz_group.code,
                pub_allowlist: dz_group
                    .pub_allowlist
                    .iter()
                    .map(|pk| pk.to_string())
                    .collect(),
                sub_allowlist: dz_group
                    .sub_allowlist
                    .iter()
                    .map(|pk| pk.to_string())
                    .collect(),
                publishers: dz_group
                    .publishers
                    .iter()
                    .map(|pk| pk.to_string())
                    .collect(),
                subscribers: dz_group
                    .subscribers
                    .iter()
                    .map(|pk| pk.to_string())
                    .collect(),
            };
            data_store
                .multicast_groups
                .insert(group.pubkey.clone(), group);
        }

        // Convert telemetry samples
        for db_sample in fetch_data.dz_telemetry.device_latency_samples {
            let sample = TelemetrySample {
                pubkey: db_sample.pubkey.to_string(),
                epoch: db_sample.epoch,
                origin_device_pk: db_sample.origin_device_pk.to_string(),
                target_device_pk: db_sample.target_device_pk.to_string(),
                link_pk: db_sample.link_pk.to_string(),
                origin_device_location_pk: db_sample.origin_device_location_pk.to_string(),
                target_device_location_pk: db_sample.target_device_location_pk.to_string(),
                origin_device_agent_pk: db_sample.origin_device_agent_pk.to_string(),
                sampling_interval_us: db_sample.sampling_interval_us,
                start_timestamp_us: db_sample.start_timestamp_us,
                samples: db_sample.samples,
                sample_count: db_sample.sample_count,
            };
            data_store.telemetry_samples.push(sample);
        }

        Ok(data_store)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Device {
    pub pubkey: String,
    pub owner: String,
    pub index: u64,
    pub bump_seed: u8,
    pub location_pubkey: Option<String>,
    pub exchange_pubkey: Option<String>,
    pub device_type: String,
    pub public_ip: String,
    pub status: String,
    pub code: String,
    pub dz_prefixes: Vec<String>,
    pub metrics_publisher_pk: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Location {
    pub pubkey: String,
    pub owner: String,
    pub index: u64,
    pub bump_seed: u8,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: u32,
    pub status: String,
    pub code: String,
    pub name: String,
    pub country: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Exchange {
    pub pubkey: String,
    pub owner: String,
    pub index: u64,
    pub bump_seed: u8,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: u32,
    pub status: String,
    pub code: String,
    pub name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Link {
    pub pubkey: String,
    pub owner: String,
    pub index: u64,
    pub bump_seed: u8,
    pub from_device_pubkey: Option<String>,
    pub to_device_pubkey: Option<String>,
    pub link_type: String,
    pub bandwidth: u64,
    pub mtu: u32,
    pub delay_ns: u64,
    pub jitter_ns: u64,
    pub tunnel_id: u16,
    pub tunnel_net: Vec<String>,
    pub status: String,
    pub code: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct User {
    pub pubkey: String,
    pub owner: String,
    pub index: u64,
    pub bump_seed: u8,
    pub user_type: String,
    pub tenant_pk: String,
    pub device_pk: Option<String>,
    pub cyoa_type: String,
    pub client_ip: String,
    pub dz_ip: String,
    pub tunnel_id: u16,
    pub tunnel_net: Vec<String>,
    pub status: String,
    pub publishers: Vec<String>,
    pub subscribers: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MulticastGroup {
    pub pubkey: String,
    pub owner: String,
    pub index: u64,
    pub bump_seed: u8,
    pub tenant_pk: String,
    pub multicast_ip: String,
    pub max_bandwidth: u64,
    pub status: String,
    pub code: String,
    pub pub_allowlist: Vec<String>,
    pub sub_allowlist: Vec<String>,
    pub publishers: Vec<String>,
    pub subscribers: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TelemetrySample {
    pub pubkey: String,
    pub epoch: u64,
    pub origin_device_pk: String,
    pub target_device_pk: String,
    pub link_pk: String,
    pub origin_device_location_pk: String,
    pub target_device_location_pk: String,
    pub origin_device_agent_pk: String,
    pub sampling_interval_us: u64,
    pub start_timestamp_us: u64,
    pub samples: Vec<u32>,
    pub sample_count: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DemandEntry {
    pub start_code: String,
    pub end_code: String,
    pub traffic: Decimal,
    pub traffic_type: i32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FetchMetadata {
    pub after_us: u64,
    pub before_us: u64,
    pub fetched_at: DateTime<Utc>,
}

impl DataStore {
    pub fn new(after_us: u64, before_us: u64) -> Self {
        Self {
            devices: HashMap::new(),
            locations: HashMap::new(),
            exchanges: HashMap::new(),
            links: HashMap::new(),
            users: HashMap::new(),
            multicast_groups: HashMap::new(),
            telemetry_samples: Vec::new(),
            demand_matrix: Vec::new(),
            metadata: FetchMetadata {
                after_us,
                before_us,
                fetched_at: Utc::now(),
            },
        }
    }

    pub fn device_count(&self) -> usize {
        self.devices.len()
    }

    pub fn location_count(&self) -> usize {
        self.locations.len()
    }

    pub fn link_count(&self) -> usize {
        self.links.len()
    }

    pub fn telemetry_sample_count(&self) -> usize {
        self.telemetry_samples.len()
    }

    pub fn get_device_location(&self, device_pubkey: &str) -> Option<&Location> {
        self.devices
            .get(device_pubkey)
            .and_then(|device| device.location_pubkey.as_ref())
            .and_then(|loc_pk| self.locations.get(loc_pk))
    }

    pub fn get_device_by_code(&self, code: &str) -> Option<&Device> {
        self.devices.values().find(|d| d.code == code)
    }

    pub fn get_location_by_code(&self, code: &str) -> Option<&Location> {
        self.locations.values().find(|l| l.code == code)
    }

    pub fn get_link_devices(&self, link: &Link) -> (Option<&Device>, Option<&Device>) {
        let from_device = link
            .from_device_pubkey
            .as_ref()
            .and_then(|pk| self.devices.get(pk));
        let to_device = link
            .to_device_pubkey
            .as_ref()
            .and_then(|pk| self.devices.get(pk));
        (from_device, to_device)
    }
}
