use std::{
    collections::BTreeMap,
    fmt::{Display, Formatter},
};

use anyhow::{Result, bail};
use chrono::{DateTime, Utc};
use doublezero_program_common::serializer;
use doublezero_serviceability::state::{
    accesspass::AccessPass as DZAccessPass,
    contributor::Contributor as DZContributor,
    device::Device as DZDevice,
    exchange::Exchange as DZExchange,
    interface::{CURRENT_INTERFACE_SCHEMA_VERSION, INTERFACE_MTU},
    link::Link as DZLink,
    location::Location as DZLocation,
    multicastgroup::MulticastGroup as DZMulticastGroup,
    user::User as DZUser,
};
use doublezero_telemetry::state::{
    device_latency_samples::DeviceLatencySamples, internet_latency_samples::InternetLatencySamples,
};
use indexmap::IndexMap;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use solana_sdk::{account::Account, pubkey::Pubkey};

pub type KeyedAccounts = Vec<(Pubkey, Account)>;

#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct FetchData {
    pub dz_serviceability: DZServiceabilityData,
    pub dz_telemetry: DZDTelemetryData,
    pub dz_internet: DZInternetData,
    /// Metro (city) prices from shred subscription program.
    /// Key: exchange pubkey, Value: price in whole USDC dollars.
    #[serde(
        default,
        serialize_with = "serializer::serialize_pubkey_btreemap",
        deserialize_with = "serializer::deserialize_pubkey_btreemap"
    )]
    pub metro_prices: BTreeMap<Pubkey, u16>,
    pub start_us: u64,
    pub end_us: u64,
    pub fetched_at: DateTime<Utc>,
}

impl Display for FetchData {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "FetchData ({} to {}): locations={}, exchanges={}, devices={}, links={}, users={}, multicast_groups={}, telemetry_samples={}, internet_samples={}, metro_prices={}",
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
            self.metro_prices.len(),
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

/// Apply compatibility migrations to snapshot/fetch-data JSON before deserializing.
///
/// This keeps snapshots produced by older binaries readable after bumping
/// `doublezero-serviceability` to account layouts with newly serialized fields.
pub fn apply_json_compat_migrations(value: &mut Value) {
    if let Some(serviceability) = value.get_mut("dz_serviceability") {
        apply_serviceability_json_compat_migrations(serviceability);
    }

    if let Some(serviceability) = value
        .get_mut("fetch_data")
        .and_then(|fetch_data| fetch_data.get_mut("dz_serviceability"))
    {
        apply_serviceability_json_compat_migrations(serviceability);
    }
}

fn apply_serviceability_json_compat_migrations(serviceability: &mut Value) {
    // doublezero-serviceability v0.19 added these Link fields. Historical
    // snapshots serialized before that bump do not contain them; default them to
    // the same values used by onchain/Borsh deserialization for absent tails.
    if let Some(links) = serviceability
        .get_mut("links")
        .and_then(|links| links.as_object_mut())
    {
        for link in links.values_mut().filter_map(|link| link.as_object_mut()) {
            link.entry("link_topologies")
                .or_insert_with(|| Value::Array(Vec::new()));
            link.entry("link_flags")
                .or_insert_with(|| Value::Number(0.into()));
        }
    }

    // doublezero-serviceability v0.20 added durable tunnel/BGP state to User;
    // client/v0.25.0 then appended `bgp_rtt_ns`, and #4030 (per-feed EdgeSeat
    // billing) appended `feed_pk`. All default to the same values onchain/Borsh
    // deserialization uses for absent tails.
    if let Some(users) = serviceability
        .get_mut("users")
        .and_then(|users| users.as_object_mut())
    {
        for user in users.values_mut().filter_map(|user| user.as_object_mut()) {
            user.entry("tunnel_endpoint")
                .or_insert_with(|| Value::String("0.0.0.0".to_string()));
            user.entry("tunnel_flags")
                .or_insert_with(|| Value::Number(0.into()));
            user.entry("bgp_status")
                .or_insert_with(|| Value::String("Unknown".to_string()));
            user.entry("last_bgp_up_at")
                .or_insert_with(|| Value::Number(0.into()));
            user.entry("last_bgp_reported_at")
                .or_insert_with(|| Value::Number(0.into()));
            user.entry("bgp_rtt_ns")
                .or_insert_with(|| Value::Number(0.into()));
            user.entry("feed_pk")
                .or_insert_with(|| Value::String(Pubkey::default().to_string()));
        }
    }

    // doublezero-serviceability client/v0.27.1 appended per-pass EdgeSeat
    // user counters/limits to AccessPass. Historical snapshots serialized
    // before that bump do not contain them; default them to the same values used
    // by onchain/Borsh deserialization for absent tails.
    if let Some(access_passes) = serviceability
        .get_mut("access_passes")
        .and_then(|access_passes| access_passes.as_object_mut())
    {
        for access_pass in access_passes
            .values_mut()
            .filter_map(|access_pass| access_pass.as_object_mut())
        {
            access_pass
                .entry("unicast_user_count")
                .or_insert_with(|| Value::Number(0.into()));
            access_pass
                .entry("max_unicast_users")
                .or_insert_with(|| Value::Number(1.into()));
            access_pass
                .entry("multicast_user_count")
                .or_insert_with(|| Value::Number(0.into()));
            access_pass
                .entry("max_multicast_users")
                .or_insert_with(|| Value::Number(1.into()));
        }
    }

    // doublezero-serviceability client/v0.25.0 split a Device's single
    // `interfaces` vec into two: a flat `interfaces: Vec<Interface>` written at
    // the end of the on-disk layout, plus a legacy
    // `deprecated_interfaces: Vec<InterfaceDeprecated>` (the `{"V1": {...}}` /
    // `{"V2": {...}}` enum) kept at the original offset for byte-compatible
    // readers. Snapshots captured under <=v0.20 carry only the legacy enum vec
    // under `interfaces`, so seed `deprecated_interfaces` from it verbatim and
    // then project `interfaces` onto the flat form. The SDK keeps both vecs the
    // same length, so seeding from the same source preserves that invariant.
    if let Some(devices) = serviceability
        .get_mut("devices")
        .and_then(|devices| devices.as_object_mut())
    {
        for device in devices
            .values_mut()
            .filter_map(|device| device.as_object_mut())
        {
            if !device.contains_key("deprecated_interfaces") {
                let legacy = device
                    .get("interfaces")
                    .cloned()
                    .unwrap_or_else(|| Value::Array(Vec::new()));
                device.insert("deprecated_interfaces".to_string(), legacy);
            }

            if let Some(interfaces) = device
                .get_mut("interfaces")
                .and_then(|interfaces| interfaces.as_array_mut())
            {
                for interface in interfaces.iter_mut() {
                    migrate_interface_to_flat(interface);
                }
            }
        }
    }
}

/// Project a legacy versioned `Interface` enum element (`{"V1": {...}}` /
/// `{"V2": {...}}`) onto the flat `Interface` struct introduced in
/// doublezero-serviceability client/v0.25.0. Mirrors the defaults stamped by the
/// SDK's `TryFrom<&InterfaceV1>` / `TryFrom<&InterfaceV2> for Interface` impls:
/// a V1 body fans in through V2, gaining `interface_cyoa`/`interface_dia` =
/// `None`, `bandwidth`/`cir` = 0, `mtu` = `INTERFACE_MTU`, `routing_mode` =
/// `Static`; both versions gain `version` = `CURRENT_INTERFACE_SCHEMA_VERSION`
/// and an empty `flex_algo_node_segments`. `size` is the on-disk byte length,
/// which offchain consumers never read (only `interface_type`/`bandwidth` are
/// used), so it is defaulted to 0 rather than recomputed.
fn migrate_interface_to_flat(interface: &mut Value) {
    let Some(obj) = interface.as_object() else {
        return;
    };
    // Already in the flat v0.25 form; nothing to do.
    if obj.contains_key("size") {
        return;
    }

    // Pull out the versioned body and note whether it's V1, which predates the
    // CYOA/DIA/bandwidth/routing fields and so needs them backfilled.
    let (is_v1, body) = if let Some(body) = obj.get("V1").and_then(Value::as_object) {
        (true, body)
    } else if let Some(body) = obj.get("V2").and_then(Value::as_object) {
        (false, body)
    } else {
        return;
    };

    let mut flat = body.clone();

    // V1 predates the CYOA/DIA/bandwidth/routing fields; backfill them with the
    // same values the V1 -> V2 conversion uses.
    if is_v1 {
        flat.entry("interface_cyoa")
            .or_insert_with(|| Value::String("None".to_string()));
        flat.entry("interface_dia")
            .or_insert_with(|| Value::String("None".to_string()));
        flat.entry("bandwidth")
            .or_insert_with(|| Value::Number(0.into()));
        flat.entry("cir").or_insert_with(|| Value::Number(0.into()));
        flat.entry("mtu")
            .or_insert_with(|| Value::Number(INTERFACE_MTU.into()));
        flat.entry("routing_mode")
            .or_insert_with(|| Value::String("Static".to_string()));
    }

    flat.entry("flex_algo_node_segments")
        .or_insert_with(|| Value::Array(Vec::new()));
    flat.insert(
        "version".to_string(),
        Value::Number(CURRENT_INTERFACE_SCHEMA_VERSION.into()),
    );
    flat.insert("size".to_string(), Value::Number(0.into()));

    *interface = Value::Object(flat);
}

/// Struct for all network data
///
/// Note: Use IndexMap to preserve insertion order during serialization/deserialization. This
/// ensures deterministic JSON output and consistent iteration order, which is critical for
/// snapshot-based reward calculations that must match R implementation exactly.
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct DZServiceabilityData {
    #[serde(
        serialize_with = "serialize_pubkey_indexmap",
        deserialize_with = "deserialize_pubkey_indexmap"
    )]
    pub locations: IndexMap<Pubkey, DZLocation>,
    #[serde(
        serialize_with = "serialize_pubkey_indexmap",
        deserialize_with = "deserialize_pubkey_indexmap"
    )]
    pub exchanges: IndexMap<Pubkey, DZExchange>,
    #[serde(
        serialize_with = "serialize_pubkey_indexmap",
        deserialize_with = "deserialize_pubkey_indexmap"
    )]
    pub devices: IndexMap<Pubkey, DZDevice>,
    #[serde(
        serialize_with = "serialize_pubkey_indexmap",
        deserialize_with = "deserialize_pubkey_indexmap"
    )]
    pub links: IndexMap<Pubkey, DZLink>,
    #[serde(
        serialize_with = "serialize_pubkey_indexmap",
        deserialize_with = "deserialize_pubkey_indexmap"
    )]
    pub users: IndexMap<Pubkey, DZUser>,
    #[serde(
        serialize_with = "serialize_pubkey_indexmap",
        deserialize_with = "deserialize_pubkey_indexmap"
    )]
    pub multicast_groups: IndexMap<Pubkey, DZMulticastGroup>,
    #[serde(
        serialize_with = "serialize_pubkey_indexmap",
        deserialize_with = "deserialize_pubkey_indexmap"
    )]
    pub contributors: IndexMap<Pubkey, DZContributor>,
    #[serde(
        serialize_with = "serializer::serialize_pubkey_btreemap",
        deserialize_with = "serializer::deserialize_pubkey_btreemap"
    )]
    pub access_passes: BTreeMap<Pubkey, DZAccessPass>,
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
    #[serde(skip)]
    pub accounts: KeyedAccounts,
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
    #[serde(skip)]
    pub accounts: KeyedAccounts,
}

/// Custom serializer for IndexMap<Pubkey, T> that preserves insertion order
/// Serializes Pubkey as string keys in JSON
pub fn serialize_pubkey_indexmap<S, T>(
    map: &IndexMap<Pubkey, T>,
    serializer: S,
) -> Result<S::Ok, S::Error>
where
    S: serde::Serializer,
    T: Serialize,
{
    use serde::ser::SerializeMap;
    let mut map_ser = serializer.serialize_map(Some(map.len()))?;
    for (k, v) in map {
        map_ser.serialize_entry(&k.to_string(), v)?;
    }
    map_ser.end()
}

/// Custom deserializer for IndexMap<Pubkey, T> that preserves insertion order
/// Deserializes from JSON object with string keys
pub fn deserialize_pubkey_indexmap<'de, D, T>(
    deserializer: D,
) -> Result<IndexMap<Pubkey, T>, D::Error>
where
    D: serde::Deserializer<'de>,
    T: Deserialize<'de>,
{
    use std::marker::PhantomData;

    use serde::de::{Error, MapAccess, Visitor};

    struct IndexMapVisitor<T>(PhantomData<T>);

    impl<'de, T> Visitor<'de> for IndexMapVisitor<T>
    where
        T: Deserialize<'de>,
    {
        type Value = IndexMap<Pubkey, T>;

        fn expecting(&self, formatter: &mut std::fmt::Formatter) -> std::fmt::Result {
            formatter.write_str("a map with Pubkey string keys")
        }

        fn visit_map<M>(self, mut access: M) -> Result<Self::Value, M::Error>
        where
            M: MapAccess<'de>,
        {
            let mut map = IndexMap::with_capacity(access.size_hint().unwrap_or(0));
            while let Some((key_str, value)) = access.next_entry::<String, T>()? {
                let key = key_str
                    .parse::<Pubkey>()
                    .map_err(|e| Error::custom(format!("Invalid Pubkey: {}", e)))?;
                map.insert(key, value);
            }
            Ok(map)
        }
    }

    deserializer.deserialize_map(IndexMapVisitor(PhantomData))
}
