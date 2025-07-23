use metrics_processor::{
    data_store::{DataStore, Device, Link, Location, TelemetrySample, User},
    processor::{MetricsProcessor, haversine_distance},
    telemetry_processor::TelemetryProcessor,
};
use rust_decimal::{Decimal, prelude::ToPrimitive};
use std::collections::HashSet;

fn create_test_data_store_full() -> DataStore {
    let mut ds = DataStore::new(1000000, 2000000);

    // Add locations
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
            name: "New York".to_string(),
            country: "US".to_string(),
        },
    );

    ds.locations.insert(
        "loc2".to_string(),
        Location {
            pubkey: "loc2".to_string(),
            owner: "owner2".to_string(),
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

    ds.locations.insert(
        "loc3".to_string(),
        Location {
            pubkey: "loc3".to_string(),
            owner: "owner3".to_string(),
            index: 3,
            bump_seed: 1,
            lat: 35.6762,
            lng: 139.6503,
            loc_id: 3,
            status: "active".to_string(),
            code: "TYO".to_string(),
            name: "Tokyo".to_string(),
            country: "JP".to_string(),
        },
    );

    // Add devices
    ds.devices.insert(
        "dev1".to_string(),
        Device {
            pubkey: "dev1".to_string(),
            owner: "operator1".to_string(),
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
            owner: "operator1".to_string(),
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

    ds.devices.insert(
        "dev3".to_string(),
        Device {
            pubkey: "dev3".to_string(),
            owner: "operator2".to_string(),
            index: 3,
            bump_seed: 1,
            location_pubkey: Some("loc3".to_string()),
            exchange_pubkey: None,
            device_type: "router".to_string(),
            public_ip: "9.10.11.12".to_string(),
            status: "activated".to_string(),
            code: "TYO-R1".to_string(),
            dz_prefixes: vec!["10.2.0.0/24".to_string()],
            metrics_publisher_pk: "pub3".to_string(),
        },
    );

    // Add private links
    ds.links.insert(
        "link1".to_string(),
        Link {
            pubkey: "link1".to_string(),
            owner: "operator1".to_string(),
            index: 1,
            bump_seed: 1,
            from_device_pubkey: Some("dev1".to_string()),
            to_device_pubkey: Some("dev2".to_string()),
            link_type: "private".to_string(),
            bandwidth: 1_000_000_000, // 1 Gbps
            mtu: 1500,
            delay_ns: 10000000,
            jitter_ns: 2000000,
            tunnel_id: 100,
            tunnel_net: vec!["192.168.1.0/30".to_string()],
            status: "activated".to_string(),
            code: "NYC-LON-1".to_string(),
        },
    );

    ds.links.insert(
        "link2".to_string(),
        Link {
            pubkey: "link2".to_string(),
            owner: "operator2".to_string(),
            index: 2,
            bump_seed: 1,
            from_device_pubkey: Some("dev2".to_string()),
            to_device_pubkey: Some("dev3".to_string()),
            link_type: "private".to_string(),
            bandwidth: 10_000_000_000, // 10 Gbps
            mtu: 1500,
            delay_ns: 50000000,
            jitter_ns: 10000000,
            tunnel_id: 101,
            tunnel_net: vec!["192.168.2.0/30".to_string()],
            status: "activated".to_string(),
            code: "LON-TYO-1".to_string(),
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
        sampling_interval_us: 1000000,
        start_timestamp_us: 1000000,
        samples: vec![10000, 11000, 10500, 12000, 10000],
        sample_count: 5,
    });

    ds.telemetry_samples.push(TelemetrySample {
        pubkey: "telemetry2".to_string(),
        epoch: 100,
        origin_device_pk: "dev2".to_string(),
        target_device_pk: "dev3".to_string(),
        link_pk: "link2".to_string(),
        origin_device_location_pk: "loc2".to_string(),
        target_device_location_pk: "loc3".to_string(),
        origin_device_agent_pk: "agent2".to_string(),
        sampling_interval_us: 1000000,
        start_timestamp_us: 1000000,
        samples: vec![50000, 51000, 49000, 52000, 50000],
        sample_count: 5,
    });

    // Add users for demand matrix
    ds.users.insert(
        "user1".to_string(),
        User {
            pubkey: "user1".to_string(),
            owner: "userowner1".to_string(),
            index: 1,
            bump_seed: 1,
            user_type: "regular".to_string(),
            tenant_pk: "tenant1".to_string(),
            device_pk: Some("dev1".to_string()),
            cyoa_type: "type1".to_string(),
            client_ip: "192.168.1.1".to_string(),
            dz_ip: "10.0.0.1".to_string(),
            tunnel_id: 200,
            tunnel_net: vec!["10.10.0.0/30".to_string()],
            status: "activated".to_string(),
            publishers: vec!["dev2".to_string()],
            subscribers: vec!["dev3".to_string()],
        },
    );

    ds
}

#[test]
fn test_device_to_location_map() {
    let ds = create_test_data_store_full();
    let processor = MetricsProcessor::new(ds);

    let map = processor.get_device_to_location_map();
    assert_eq!(map.get("NYC-R1"), Some(&"NYC".to_string()));
    assert_eq!(map.get("LON-R1"), Some(&"LON".to_string()));
    assert_eq!(map.get("TYO-R1"), Some(&"TYO".to_string()));
}

#[test]
fn test_device_to_operator_map() {
    let ds = create_test_data_store_full();
    let processor = MetricsProcessor::new(ds);

    let map = processor.get_device_to_operator_map();
    assert_eq!(map.get("NYC-R1"), Some(&"operator1".to_string()));
    assert_eq!(map.get("LON-R1"), Some(&"operator1".to_string()));
    assert_eq!(map.get("TYO-R1"), Some(&"operator2".to_string()));
}

#[test]
fn test_process_private_links() {
    let ds = create_test_data_store_full();
    let processor = MetricsProcessor::new(ds);

    let telemetry_stats = TelemetryProcessor::calculate_all_stats(processor.get_data_store());
    let private_links = processor.process_private_links(&telemetry_stats).unwrap();

    assert_eq!(private_links.len(), 2);

    // Check first link
    let link1 = private_links
        .iter()
        .find(|l| l.device1 == "NYC-R1" && l.device2 == "LON-R1");
    assert!(link1.is_some());
    let link1 = link1.unwrap();
    assert_eq!(link1.bandwidth, 1.0); // 1 Gbps
    assert!(link1.latency > 0.0);

    // Check second link
    let link2 = private_links
        .iter()
        .find(|l| l.device1 == "LON-R1" && l.device2 == "TYO-R1");
    assert!(link2.is_some());
    let link2 = link2.unwrap();
    assert_eq!(link2.bandwidth, 10.0); // 10 Gbps
    assert!(link2.latency > 0.0);
}

#[test]
fn test_generate_public_links() {
    let ds = create_test_data_store_full();
    let processor = MetricsProcessor::new(ds);

    let mut switches = HashSet::new();
    switches.insert("NYC-R1".to_string());
    switches.insert("LON-R1".to_string());
    switches.insert("TYO-R1".to_string());

    let device_to_location = processor.get_device_to_location_map();
    let public_links = processor
        .generate_public_links_for_switches(&switches, &device_to_location)
        .unwrap();

    // Should have 3 links for 3 devices: NYC-LON, NYC-TYO, LON-TYO
    assert_eq!(public_links.len(), 3);

    // Check all links exist (public links use city codes, not device codes)
    let has_nyc_lon = public_links
        .iter()
        .any(|l| (l.city1 == "NYC" && l.city2 == "LON") || (l.city1 == "LON" && l.city2 == "NYC"));
    assert!(has_nyc_lon);

    let has_nyc_tyo = public_links
        .iter()
        .any(|l| (l.city1 == "NYC" && l.city2 == "TYO") || (l.city1 == "TYO" && l.city2 == "NYC"));
    assert!(has_nyc_tyo);

    let has_lon_tyo = public_links
        .iter()
        .any(|l| (l.city1 == "LON" && l.city2 == "TYO") || (l.city1 == "TYO" && l.city2 == "LON"));
    assert!(has_lon_tyo);
}

#[test]
fn test_calculate_demand_matrix() {
    let ds = create_test_data_store_full();
    let processor = MetricsProcessor::new(ds);

    let demand = processor.calculate_demand_matrix().unwrap();

    // Should have demands based on user publishers/subscribers
    assert!(!demand.is_empty());

    // Check specific demands
    let pub_demand = demand
        .iter()
        .find(|d| d.start == "LON-R1" && d.end == "NYC-R1");
    assert!(pub_demand.is_some());
    assert_eq!(pub_demand.unwrap().traffic, 0.5);

    // Note: demands are transformed to location codes in process_metrics
    // So the actual demand will be between location codes, not device codes
}

#[test]
fn test_process_metrics_full() {
    let ds = create_test_data_store_full();
    let processor = MetricsProcessor::new(ds);

    let (shapley_inputs, processed_metrics) = processor.process_metrics().unwrap();

    // Check processed metrics
    assert_eq!(processed_metrics.private_links_count, 2);
    assert_eq!(processed_metrics.public_links_count, 3);
    assert!(processed_metrics.demand_entries_count > 0);
    assert_eq!(processed_metrics.telemetry_stats_count, 2);

    // Check shapley inputs
    assert_eq!(shapley_inputs.private_links.len(), 2);
    assert_eq!(shapley_inputs.public_links.len(), 3);
    assert!(!shapley_inputs.demand_matrix.is_empty());
    assert_eq!(
        shapley_inputs.demand_multiplier,
        Decimal::from_str_exact("1.2").unwrap()
    );
}

#[test]
fn test_haversine_distance() {
    // NYC to London
    let distance = haversine_distance(40.7128, -74.0060, 51.5074, -0.1278);
    assert!(distance > 5500.0 && distance < 5600.0); // ~5570 km

    // Same location
    let distance = haversine_distance(40.7128, -74.0060, 40.7128, -74.0060);
    assert!(distance < 0.1);
}

#[test]
fn test_baseline_generation() {
    let ds = create_test_data_store_full();
    let processor = MetricsProcessor::new(ds);

    let baseline = processor.find_or_generate_baseline("NYC", "LON");

    // Check distance-based calculations
    assert!(baseline.distance_km.to_f64().unwrap() > 5500.0);
    assert!(baseline.latency_ms.to_f64().unwrap() > 50.0);
    assert!(baseline.jitter_ms.to_f64().unwrap() > 10.0);
    assert!(baseline.packet_loss.to_f64().unwrap() > 0.001);
    assert!(baseline.bandwidth_mbps.to_f64().unwrap() < 100.0);
}
