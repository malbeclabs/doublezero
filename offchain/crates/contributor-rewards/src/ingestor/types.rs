use anyhow::{Result, bail};
use chrono::{DateTime, Utc};
use doublezero_program_common::serializer;
use doublezero_serviceability::state::{
    contributor::Contributor as DZContributor, device::Device as DZDevice,
    exchange::Exchange as DZExchange, link::Link as DZLink, location::Location as DZLocation,
    multicastgroup::MulticastGroup as DZMulticastGroup, user::User as DZUser,
};
use doublezero_telemetry::state::{
    device_latency_samples::DeviceLatencySamples, internet_latency_samples::InternetLatencySamples,
};
use serde::{Deserialize, Serialize};
use solana_sdk::pubkey::Pubkey;
use std::{
    collections::BTreeMap,
    fmt::{Display, Formatter},
};

#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct FetchData {
    pub dz_serviceability: DZServiceabilityData,
    pub dz_telemetry: DZDTelemetryData,
    pub dz_internet: DZInternetData,
    pub start_us: u64,
    pub end_us: u64,
    pub fetched_at: DateTime<Utc>,
}

impl Display for FetchData {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "FetchData ({} to {}): locations={}, exchanges={}, devices={}, links={}, users={}, multicast_groups={}, telemetry_samples={}, internet_samples={}",
            self.start_us,
            self.end_us,
            self.dz_serviceability.locations.len(),
            self.dz_serviceability.exchanges.len(),
            self.dz_serviceability.devices.len(),
            self.dz_serviceability.links.len(),
            self.dz_serviceability.users.len(),
            self.dz_serviceability.multicast_groups.len(),
            self.dz_telemetry.device_latency_samples.len(),
            self.dz_internet.internet_latency_samples.len(),
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
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct DZServiceabilityData {
    #[serde(
        serialize_with = "serializer::serialize_pubkey_btreemap",
        deserialize_with = "serializer::deserialize_pubkey_btreemap"
    )]
    pub locations: BTreeMap<Pubkey, DZLocation>,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_btreemap",
        deserialize_with = "serializer::deserialize_pubkey_btreemap"
    )]
    pub exchanges: BTreeMap<Pubkey, DZExchange>,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_btreemap",
        deserialize_with = "serializer::deserialize_pubkey_btreemap"
    )]
    pub devices: BTreeMap<Pubkey, DZDevice>,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_btreemap",
        deserialize_with = "serializer::deserialize_pubkey_btreemap"
    )]
    pub links: BTreeMap<Pubkey, DZLink>,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_btreemap",
        deserialize_with = "serializer::deserialize_pubkey_btreemap"
    )]
    pub users: BTreeMap<Pubkey, DZUser>,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_btreemap",
        deserialize_with = "serializer::deserialize_pubkey_btreemap"
    )]
    pub multicast_groups: BTreeMap<Pubkey, DZMulticastGroup>,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_btreemap",
        deserialize_with = "serializer::deserialize_pubkey_btreemap"
    )]
    pub contributors: BTreeMap<Pubkey, DZContributor>,
}

/// DB representation of DeviceLatencySamples
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DZDeviceLatencySamples {
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub pubkey: Pubkey,
    pub epoch: u64,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub origin_device_pk: Pubkey,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub target_device_pk: Pubkey,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub link_pk: Pubkey,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub origin_device_location_pk: Pubkey,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub target_device_location_pk: Pubkey,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
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
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct DZDTelemetryData {
    pub device_latency_samples: Vec<DZDeviceLatencySamples>,
}

impl DZDTelemetryData {
    pub fn start_end_us(&self) -> Result<(u64, u64)> {
        let mut min_timestamp = u64::MAX;
        let mut max_timestamp = 0u64;
        for sample in &self.device_latency_samples {
            min_timestamp = min_timestamp.min(sample.start_timestamp_us);
            let end_timestamp = sample.start_timestamp_us
                + (sample.sample_count as u64 * sample.sampling_interval_us);
            max_timestamp = max_timestamp.max(end_timestamp);
        }

        if min_timestamp == u64::MAX {
            bail!("Incorrect start_us (min_timestamp) for telemetry data!")
        }
        if max_timestamp == 0u64 {
            bail!("Incorrect end_us (max_timestamp) for telemetry data!")
        }

        Ok((min_timestamp, max_timestamp))
    }
}

/// Representation of InternetLatencySamples
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DZInternetLatencySamples {
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub pubkey: Pubkey,
    pub epoch: u64,
    pub data_provider_name: String,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub oracle_agent_pk: Pubkey,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub origin_exchange_pk: Pubkey,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_as_string",
        deserialize_with = "serializer::deserialize_pubkey_from_string"
    )]
    pub target_exchange_pk: Pubkey,
    pub sampling_interval_us: u64,
    pub start_timestamp_us: u64,
    pub samples: Vec<u32>,
    pub sample_count: u32,
}

impl DZInternetLatencySamples {
    pub fn from_raw(pubkey: Pubkey, samples: &InternetLatencySamples) -> Self {
        Self {
            pubkey,
            epoch: samples.header.epoch,
            data_provider_name: samples.header.data_provider_name.to_string(),
            oracle_agent_pk: samples.header.oracle_agent_pk,
            origin_exchange_pk: samples.header.origin_exchange_pk,
            target_exchange_pk: samples.header.target_exchange_pk,
            sampling_interval_us: samples.header.sampling_interval_microseconds,
            start_timestamp_us: samples.header.start_timestamp_microseconds,
            samples: samples.samples.clone(),
            sample_count: samples.header.next_sample_index,
        }
    }
}

/// Telemetry data container
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct DZInternetData {
    pub internet_latency_samples: Vec<DZInternetLatencySamples>,
}
