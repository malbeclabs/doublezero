use anyhow::Result;
use chrono::{DateTime, Utc};
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};
use std::{collections::HashMap, path::Path};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DataStore {
    pub devices: HashMap<String, Device>,
    pub locations: HashMap<String, Location>,
    pub exchanges: HashMap<String, Exchange>,
    pub links: HashMap<String, Link>,
    pub users: HashMap<String, User>,
    pub multicast_groups: HashMap<String, MulticastGroup>,
    pub telemetry_samples: Vec<TelemetrySample>,
    pub internet_baselines: Vec<InternetBaseline>,
    pub demand_matrix: Vec<DemandEntry>,
    pub metadata: FetchMetadata,
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
pub struct InternetBaseline {
    pub from_location_code: String,
    pub to_location_code: String,
    pub from_lat: f64,
    pub from_lng: f64,
    pub to_lat: f64,
    pub to_lng: f64,
    pub distance_km: Decimal,
    pub latency_ms: Decimal,
    pub jitter_ms: Decimal,
    pub packet_loss: Decimal,
    pub bandwidth_mbps: Decimal,
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
            internet_baselines: Vec::new(),
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

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CachedData {
    pub version: String,
    pub timestamp: DateTime<Utc>,
    pub data_store: DataStore,
    pub processed_metrics: Option<ProcessedMetrics>,
    pub shapley_inputs: Option<crate::shapley_types::ShapleyInputs>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProcessedMetrics {
    pub private_links_count: usize,
    pub public_links_count: usize,
    pub demand_entries_count: usize,
    pub telemetry_stats_count: usize,
}

impl CachedData {
    pub fn new(data_store: DataStore) -> Self {
        Self {
            version: env!("CARGO_PKG_VERSION").to_string(),
            timestamp: Utc::now(),
            data_store,
            processed_metrics: None,
            shapley_inputs: None,
        }
    }

    pub fn save_to_json(&self, path: &Path) -> Result<()> {
        let json = serde_json::to_string_pretty(self)?;
        std::fs::create_dir_all(path.parent().unwrap_or(Path::new(".")))?;
        std::fs::write(path, json)?;
        Ok(())
    }

    pub fn load_from_json(path: &Path) -> Result<Self> {
        let json = std::fs::read_to_string(path)?;
        let cached: CachedData = serde_json::from_str(&json)?;
        Ok(cached)
    }
}
