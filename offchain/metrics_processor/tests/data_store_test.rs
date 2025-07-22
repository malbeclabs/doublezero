use metrics_processor::data_store::{
    CachedData, DataStore, Device, Link, Location, TelemetrySample,
};
use tempfile::TempDir;

fn create_test_data_store() -> DataStore {
    let mut ds = DataStore::new(1000000, 2000000);

    // Add test locations
    ds.locations.insert(
        "loc1".to_string(),
        Location {
            pubkey: "loc1".to_string(),
            owner: "owner1".to_string(),
            index: 1,
            bump_seed: 1,
            lat: 40.7128,
            lng: -74.0060,
            loc_id: 1,
            status: "active".to_string(),
            code: "NYC".to_string(),
            name: "New York City".to_string(),
            country: "US".to_string(),
        },
    );

    ds.locations.insert(
        "loc2".to_string(),
        Location {
            pubkey: "loc2".to_string(),
            owner: "owner1".to_string(),
            index: 2,
            bump_seed: 1,
            lat: 51.5074,
            lng: -0.1278,
            loc_id: 2,
            status: "active".to_string(),
            code: "LON".to_string(),
            name: "London".to_string(),
            country: "UK".to_string(),
        },
    );

    // Add test devices
    ds.devices.insert(
        "dev1".to_string(),
        Device {
            pubkey: "dev1".to_string(),
            owner: "owner1".to_string(),
            index: 1,
            bump_seed: 1,
            location_pubkey: Some("loc1".to_string()),
            exchange_pubkey: None,
            device_type: "router".to_string(),
            public_ip: "1.2.3.4".to_string(),
            status: "activated".to_string(),
            code: "NYC-R1".to_string(),
            dz_prefixes: vec!["10.0.0.0/24".to_string()],
            metrics_publisher_pk: "pub1".to_string(),
        },
    );

    ds.devices.insert(
        "dev2".to_string(),
        Device {
            pubkey: "dev2".to_string(),
            owner: "owner2".to_string(),
            index: 2,
            bump_seed: 1,
            location_pubkey: Some("loc2".to_string()),
            exchange_pubkey: None,
            device_type: "router".to_string(),
            public_ip: "5.6.7.8".to_string(),
            status: "activated".to_string(),
            code: "LON-R1".to_string(),
            dz_prefixes: vec!["10.1.0.0/24".to_string()],
            metrics_publisher_pk: "pub2".to_string(),
        },
    );

    // Add test link
    ds.links.insert(
        "link1".to_string(),
        Link {
            pubkey: "link1".to_string(),
            owner: "owner1".to_string(),
            index: 1,
            bump_seed: 1,
            from_device_pubkey: Some("dev1".to_string()),
            to_device_pubkey: Some("dev2".to_string()),
            link_type: "private".to_string(),
            bandwidth: 1000000000, // 1 Gbps
            mtu: 1500,
            delay_ns: 10000000, // 10ms
            jitter_ns: 2000000, // 2ms
            tunnel_id: 100,
            tunnel_net: vec!["192.168.1.0/30".to_string()],
            status: "active".to_string(),
            code: "NYC-LON-1".to_string(),
        },
    );

    // Add telemetry samples
    ds.telemetry_samples.push(TelemetrySample {
        pubkey: "telemetry1".to_string(),
        epoch: 100,
        origin_device_pk: "dev1".to_string(),
        target_device_pk: "dev2".to_string(),
        link_pk: "link1".to_string(),
        origin_device_location_pk: "loc1".to_string(),
        target_device_location_pk: "loc2".to_string(),
        origin_device_agent_pk: "agent1".to_string(),
        sampling_interval_us: 1000000, // 1 second
        start_timestamp_us: 1000000,
        samples: vec![10000, 11000, 10500, 12000, 10000], // latencies in microseconds
        sample_count: 5,
    });

    ds
}

#[test]
fn test_data_store_creation() {
    let ds = DataStore::new(1000, 2000);
    assert_eq!(ds.metadata.after_us, 1000);
    assert_eq!(ds.metadata.before_us, 2000);
    assert_eq!(ds.device_count(), 0);
    assert_eq!(ds.location_count(), 0);
    assert_eq!(ds.link_count(), 0);
    assert_eq!(ds.telemetry_sample_count(), 0);
}

#[test]
fn test_device_location_lookup() {
    let ds = create_test_data_store();

    let location = ds.get_device_location("dev1");
    assert!(location.is_some());
    assert_eq!(location.unwrap().code, "NYC");

    let location = ds.get_device_location("dev2");
    assert!(location.is_some());
    assert_eq!(location.unwrap().code, "LON");

    let location = ds.get_device_location("nonexistent");
    assert!(location.is_none());
}

#[test]
fn test_device_by_code_lookup() {
    let ds = create_test_data_store();

    let device = ds.get_device_by_code("NYC-R1");
    assert!(device.is_some());
    assert_eq!(device.unwrap().pubkey, "dev1");

    let device = ds.get_device_by_code("LON-R1");
    assert!(device.is_some());
    assert_eq!(device.unwrap().pubkey, "dev2");

    let device = ds.get_device_by_code("UNKNOWN");
    assert!(device.is_none());
}

#[test]
fn test_link_devices_lookup() {
    let ds = create_test_data_store();
    let link = ds.links.get("link1").unwrap();

    let (from_device, to_device) = ds.get_link_devices(link);
    assert!(from_device.is_some());
    assert!(to_device.is_some());
    assert_eq!(from_device.unwrap().code, "NYC-R1");
    assert_eq!(to_device.unwrap().code, "LON-R1");
}

#[test]
fn test_json_serialization() {
    let ds = create_test_data_store();
    let cached = CachedData::new(ds);

    // Test serialization
    let json = serde_json::to_string_pretty(&cached).unwrap();
    assert!(json.contains("NYC-R1"));
    assert!(json.contains("LON-R1"));
    assert!(json.contains("telemetry1"));

    // Test deserialization
    let deserialized: CachedData = serde_json::from_str(&json).unwrap();
    assert_eq!(deserialized.data_store.device_count(), 2);
    assert_eq!(deserialized.data_store.location_count(), 2);
    assert_eq!(deserialized.data_store.link_count(), 1);
    assert_eq!(deserialized.data_store.telemetry_sample_count(), 1);
}

#[test]
fn test_save_and_load_json() {
    let temp_dir = TempDir::new().unwrap();
    let cache_path = temp_dir.path().join("cache.json");

    let ds = create_test_data_store();
    let cached = CachedData::new(ds);

    // Save to file
    cached.save_to_json(&cache_path).unwrap();
    assert!(cache_path.exists());

    // Load from file
    let loaded = CachedData::load_from_json(&cache_path).unwrap();
    assert_eq!(loaded.data_store.device_count(), 2);
    assert_eq!(loaded.data_store.location_count(), 2);
    assert_eq!(loaded.data_store.link_count(), 1);
    assert_eq!(loaded.data_store.telemetry_sample_count(), 1);

    // Verify specific data
    let device = loaded.data_store.get_device_by_code("NYC-R1");
    assert!(device.is_some());
    assert_eq!(device.unwrap().public_ip, "1.2.3.4");
}

#[test]
fn test_counts() {
    let ds = create_test_data_store();
    assert_eq!(ds.device_count(), 2);
    assert_eq!(ds.location_count(), 2);
    assert_eq!(ds.link_count(), 1);
    assert_eq!(ds.telemetry_sample_count(), 1);
}
