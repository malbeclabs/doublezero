use chrono::Utc;
use metrics_processor::{
    data_converter::convert_to_datastore,
    types::{
        DbDevice, DbDeviceLatencySamples, DbLocation, NetworkData, RewardsData, TelemetryData,
    },
};
use solana_sdk::pubkey::Pubkey;

#[test]
fn test_convert_simple_network_data() {
    // Create test data
    let mut network_data = NetworkData::default();

    // Add a location
    let loc_pubkey = Pubkey::new_unique();
    let loc = DbLocation {
        pubkey: loc_pubkey,
        owner: Pubkey::new_unique(),
        index: 1,
        bump_seed: 255,
        lat: 40.7128,
        lng: -74.0060,
        loc_id: 1,
        status: "activated".to_string(),
        code: "NYC".to_string(),
        name: "New York City".to_string(),
        country: "US".to_string(),
    };
    network_data.locations.push(loc.clone());

    // Add a device
    let dev_pubkey = Pubkey::new_unique();
    let dev = DbDevice {
        pubkey: dev_pubkey,
        owner: Pubkey::new_unique(),
        index: 1,
        bump_seed: 255,
        location_pubkey: Some(loc_pubkey),
        exchange_pubkey: None,
        device_type: "gateway".to_string(),
        public_ip: "1.2.3.4".to_string(),
        status: "activated".to_string(),
        code: "DEV001".to_string(),
        dz_prefixes: serde_json::json!([]),
        metrics_publisher_pk: Pubkey::new_unique(),
    };
    network_data.devices.push(dev.clone());

    // Create rewards data
    let rewards_data = RewardsData {
        network: network_data,
        telemetry: TelemetryData::default(),
        after_us: 0,
        before_us: 1000000,
        fetched_at: Utc::now(),
    };

    // Convert to data store
    let data_store = convert_to_datastore(rewards_data).unwrap();

    // Verify conversions
    assert_eq!(data_store.locations.len(), 1);
    assert_eq!(data_store.devices.len(), 1);

    let stored_loc = data_store.locations.get(&loc_pubkey.to_string()).unwrap();
    assert_eq!(stored_loc.code, "NYC");
    assert_eq!(stored_loc.lat, 40.7128);

    let stored_dev = data_store.devices.get(&dev_pubkey.to_string()).unwrap();
    assert_eq!(stored_dev.code, "DEV001");
    assert_eq!(stored_dev.location_pubkey, Some(loc_pubkey.to_string()));
}

#[test]
fn test_convert_telemetry_data() {
    let sample_pubkey = Pubkey::new_unique();
    let dev1_pubkey = Pubkey::new_unique();
    let dev2_pubkey = Pubkey::new_unique();
    let link_pubkey = Pubkey::new_unique();

    let telemetry_sample = DbDeviceLatencySamples {
        pubkey: sample_pubkey,
        epoch: 100,
        origin_device_pk: dev1_pubkey,
        target_device_pk: dev2_pubkey,
        link_pk: link_pubkey,
        origin_device_location_pk: Pubkey::new_unique(),
        target_device_location_pk: Pubkey::new_unique(),
        origin_device_agent_pk: Pubkey::new_unique(),
        sampling_interval_us: 5000000, // 5 seconds
        start_timestamp_us: 1000000,
        samples: vec![100, 150, 200, 250, 300],
        sample_count: 5,
    };

    let rewards_data = RewardsData {
        network: NetworkData::default(),
        telemetry: TelemetryData {
            device_latency_samples: vec![telemetry_sample.clone()],
        },
        after_us: 0,
        before_us: 2000000,
        fetched_at: Utc::now(),
    };

    let data_store = convert_to_datastore(rewards_data).unwrap();

    assert_eq!(data_store.telemetry_samples.len(), 1);
    let stored_sample = &data_store.telemetry_samples[0];
    assert_eq!(stored_sample.pubkey, sample_pubkey.to_string());
    assert_eq!(stored_sample.epoch, 100);
    assert_eq!(stored_sample.samples, vec![100, 150, 200, 250, 300]);
}
