use chrono::{DateTime, Utc};
use doublezero_serviceability::state::{
    contributor::Contributor as DZContributor, device::Device as DZDevice,
    exchange::Exchange as DZExchange, link::Link as DZLink, location::Location as DZLocation,
    multicastgroup::MulticastGroup as DZMulticastGroup, user::User as DZUser,
};
use doublezero_telemetry::state::device_latency_samples::DeviceLatencySamples;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{
    collections::HashMap,
    fmt::{Display, Formatter},
};

#[derive(Debug, Default, Clone, Serialize)]
pub struct FetchData {
    pub dz_serviceability: DZServiceabilityData,
    pub dz_telemetry: DZDTelemetryData,
    pub after_us: u64,
    pub before_us: u64,
    pub fetched_at: DateTime<Utc>,
}

impl Display for FetchData {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "FetchData ({} to {}): locations={}, exchanges={}, devices={}, links={}, users={}, multicast_groups={}, telemetry_samples={}",
            self.after_us,
            self.before_us,
            self.dz_serviceability.locations.len(),
            self.dz_serviceability.exchanges.len(),
            self.dz_serviceability.devices.len(),
            self.dz_serviceability.links.len(),
            self.dz_serviceability.users.len(),
            self.dz_serviceability.multicast_groups.len(),
            self.dz_telemetry.device_latency_samples.len(),
        )
    }
}

impl FetchData {
    pub fn get_device_location(&self, device_pubkey: &Pubkey) -> Option<&DZLocation> {
        self.dz_serviceability
            .devices
            .get(device_pubkey)
            .map(|device| device.location_pk)
            .and_then(|loc_pk| self.dz_serviceability.locations.get(&loc_pk))
    }

    pub fn get_device_by_code(&self, code: &str) -> Option<&DZDevice> {
        self.dz_serviceability
            .devices
            .values()
            .find(|d| d.code == code)
    }

    pub fn get_location_by_code(&self, code: &str) -> Option<&DZLocation> {
        self.dz_serviceability
            .locations
            .values()
            .find(|l| l.code == code)
    }

    pub fn get_link_devices(&self, link: &DZLink) -> (Option<&DZDevice>, Option<&DZDevice>) {
        let from_device = self.dz_serviceability.devices.get(&link.side_a_pk);
        let to_device = self.dz_serviceability.devices.get(&link.side_z_pk);
        (from_device, to_device)
    }
}

/// Struct for all network data
#[derive(Debug, Default, Clone, Serialize)]
pub struct DZServiceabilityData {
    pub locations: HashMap<Pubkey, DZLocation>,
    pub exchanges: HashMap<Pubkey, DZExchange>,
    pub devices: HashMap<Pubkey, DZDevice>,
    pub links: HashMap<Pubkey, DZLink>,
    pub users: HashMap<Pubkey, DZUser>,
    pub multicast_groups: HashMap<Pubkey, DZMulticastGroup>,
    pub contributors: HashMap<Pubkey, DZContributor>,
}

/// DB representation of DeviceLatencySamples
#[derive(Debug, Clone, Serialize)]
pub struct DZDeviceLatencySamples {
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

impl DZDeviceLatencySamples {
    pub fn from_raw(pubkey: Pubkey, samples: &DeviceLatencySamples) -> Self {
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
#[derive(Debug, Default, Clone, Serialize)]
pub struct DZDTelemetryData {
    pub device_latency_samples: Vec<DZDeviceLatencySamples>,
}
