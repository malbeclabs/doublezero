use crate::servicecontroller::{LatencyRecord, ServiceController};
use backon::{ExponentialBuilder, Retryable};
use doublezero_sdk::{Device, DeviceStatus};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, net::Ipv4Addr, str::FromStr, time::Duration};

/// Get all tunnel endpoints for a device.
/// Returns the device's public_ip plus any UserTunnelEndpoint interface IPs.
fn get_device_tunnel_endpoints(device: &Device) -> Vec<Ipv4Addr> {
    let mut endpoints = vec![device.public_ip];

    // Add all UserTunnelEndpoint interfaces
    for iface in &device.interfaces {
        let iface = iface.into_current_version();
        if iface.user_tunnel_endpoint && iface.ip_net != Default::default() {
            endpoints.push(iface.ip_net.ip());
        }
    }

    endpoints
}

/// Check if a device has any available tunnel endpoint that is not in the exclude list.
/// Returns true if the device has at least one endpoint not in exclude_ips.
fn device_has_available_endpoint(device: &Device, exclude_ips: &[Ipv4Addr]) -> bool {
    let endpoints = get_device_tunnel_endpoints(device);
    endpoints.iter().any(|ep| !exclude_ips.contains(ep))
}

/// Select the best tunnel endpoint for the given device based on latency data.
/// Returns the lowest-latency endpoint IP not in `exclude_ips`.
/// Falls back to Ipv4Addr::UNSPECIFIED if no per-endpoint data is available.
pub fn select_tunnel_endpoint(
    latencies: &[LatencyRecord],
    device_pk: &str,
    device_public_ip: Ipv4Addr,
    exclude_ips: &[Ipv4Addr],
) -> Ipv4Addr {
    // Filter latencies to records matching this device_pk, sorted by avg latency (ascending)
    let mut device_latencies: Vec<&LatencyRecord> = latencies
        .iter()
        .filter(|l| l.device_pk == device_pk)
        .collect();
    device_latencies.sort_by(|a, b| {
        a.avg_latency_ns
            .partial_cmp(&b.avg_latency_ns)
            .unwrap_or(std::cmp::Ordering::Equal)
    });

    // Pick the first endpoint not in exclude_ips
    for latency in &device_latencies {
        if let Ok(ip) = latency.device_ip.parse::<Ipv4Addr>() {
            if !exclude_ips.contains(&ip) {
                return ip;
            }
        }
    }

    // Fallback: if the device's public_ip is not excluded, use it
    if !exclude_ips.contains(&device_public_ip) && device_public_ip != Ipv4Addr::UNSPECIFIED {
        return device_public_ip;
    }

    // No suitable endpoint found; let the activator pick one
    Ipv4Addr::UNSPECIFIED
}

pub async fn retrieve_latencies<T: ServiceController>(
    controller: &T,
    devices: &HashMap<Pubkey, Device>,
    reachable_only: bool,
    spinner: Option<&indicatif::ProgressBar>,
) -> eyre::Result<Vec<LatencyRecord>> {
    if let Some(spinner) = spinner {
        spinner.set_message("Retrieving latency stats...");
    }

    let get_latencies = || async {
        let mut latencies = controller.latency().await.map_err(|e| eyre::eyre!(e))?;
        latencies.retain(|l| {
            Pubkey::from_str(&l.device_pk)
                .ok()
                .and_then(|pubkey| devices.get(&pubkey))
                .map(|device| device.status == DeviceStatus::Activated)
                .unwrap_or(false)
        });

        if reachable_only {
            latencies.retain(|l| l.reachable);
        }

        match latencies.len() {
            0 => Err(eyre::eyre!("No devices found")),
            _ => Ok(latencies),
        }
    };

    let builder = ExponentialBuilder::new()
        .with_max_times(5)
        .with_min_delay(Duration::from_secs(1))
        .with_max_delay(Duration::from_secs(10));

    let mut latencies = get_latencies
        .retry(builder)
        .when(|e| e.to_string() == "No devices found")
        .notify(|_, dur| {
            if let Some(spinner) = spinner {
                spinner.set_message(format!("Waiting for latency stats after {dur:?}"));
            }
        })
        .await?;

    latencies.sort_by(|a, b| {
        let reachable_cmp = b.reachable.cmp(&a.reachable);
        if reachable_cmp != std::cmp::Ordering::Equal {
            return reachable_cmp;
        }
        a.avg_latency_ns
            .partial_cmp(&b.avg_latency_ns)
            .unwrap_or(std::cmp::Ordering::Equal)
    });

    Ok(latencies)
}

// Latency tolerance when preferring the current device or avoiding IP collisions.
//
// NOTE: This was previously 1_500_000 ns (1.5 ms). It was increased to 5_000_000 ns
// (5 ms) to better support scenarios with multiple concurrent tunnels and a larger
// device pool:
//   * With multiple tunnels, we sometimes need to pick a different device (e.g.,
//     to avoid reusing the same endpoint IP) even if its latency is slightly worse.
//   * A stricter tolerance (1.5 ms) caused the current/closest device to be
//     "sticky" in many real-world network conditions, preventing effective load
//     distribution across devices for additional tunnels.
//   * A 5 ms window still keeps selections within a "nearby" latency band for
//     typical internet connections while giving the selector enough freedom to
//     choose alternate devices when needed.
//
// The value is expressed in nanoseconds to match avg_latency_ns.
const LATENCY_TOLERANCE_NS: i32 = 5_000_000; // 5 ms

/// Find the best device based on latency.
///
/// # Arguments
/// * `controller` - Service controller for fetching latency data
/// * `devices` - Map of device pubkeys to devices
/// * `ignore_unprovisionable` - If true, skip devices that can't accept new users
/// * `spinner` - Optional progress spinner for UI feedback
/// * `current_device` - Optional current device pubkey (preferred within tolerance)
/// * `exclude_ips` - IPs to exclude (e.g., user's existing tunnel endpoints to ensure
///   multiple tunnels go to different devices)
pub async fn best_latency<T: ServiceController>(
    controller: &T,
    devices: &HashMap<Pubkey, Device>,
    ignore_unprovisionable: bool,
    spinner: Option<&indicatif::ProgressBar>,
    current_device: Option<&Pubkey>,
    exclude_ips: &[Ipv4Addr],
) -> eyre::Result<LatencyRecord> {
    let mut latencies = retrieve_latencies(controller, devices, true, spinner).await?;

    // Filter out devices where ALL tunnel endpoints are already in use by this client.
    // A device should only be excluded if it has no remaining available endpoints.
    if !exclude_ips.is_empty() {
        latencies.retain(|l| {
            let device_pk = match Pubkey::from_str(&l.device_pk) {
                Ok(pk) => pk,
                Err(_) => return true, // Keep if we can't parse pubkey
            };
            let device = match devices.get(&device_pk) {
                Some(d) => d,
                None => return true, // Keep if device not found
            };
            // Keep this device if it has at least one available endpoint
            device_has_available_endpoint(device, exclude_ips)
        });
    }

    if latencies.is_empty() {
        return Err(eyre::eyre!("No suitable device found after filtering"));
    }
    let mut best: Option<&LatencyRecord> = None;
    let mut best_latency = i32::MAX;

    if let Some(current_device) = current_device {
        if let Some(current) = latencies
            .iter()
            .find(|latency| latency.device_pk == current_device.to_string())
        {
            best = Some(current);
            best_latency = current.avg_latency_ns;
        }
    }

    for latency in &latencies {
        if let Some(current) = best {
            if std::ptr::eq(current, latency) {
                return Ok(current.clone());
            }
        }

        let device_pk = Pubkey::from_str(&latency.device_pk)?;
        let device = devices
            .get(&device_pk)
            .ok_or_else(|| eyre::eyre!("Device with pubkey {} not found", &latency.device_pk))?;

        if (!ignore_unprovisionable || device.is_device_eligible_for_provisioning())
            && (latency.avg_latency_ns - best_latency).abs() > LATENCY_TOLERANCE_NS
        {
            best = Some(latency);
            break;
        }
    }

    best.cloned()
        .ok_or_else(|| eyre::eyre!("No suitable device found"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::servicecontroller::{LatencyRecord, MockServiceController};
    use doublezero_program_common::types::{NetworkV4, NetworkV4List};
    use doublezero_sdk::{
        AccountType, CurrentInterfaceVersion, Device, DeviceStatus, DeviceType, Interface,
        InterfaceStatus, InterfaceType, LoopbackType,
    };
    use doublezero_serviceability::state::interface::{InterfaceCYOA, InterfaceDIA, RoutingMode};
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    fn make_device(status: DeviceStatus, users_count: u16) -> (Pubkey, Device) {
        make_device_with_ip(status, users_count, std::net::Ipv4Addr::UNSPECIFIED, vec![])
    }

    fn make_device_with_ip(
        status: DeviceStatus,
        users_count: u16,
        public_ip: Ipv4Addr,
        tunnel_endpoint_ips: Vec<Ipv4Addr>,
    ) -> (Pubkey, Device) {
        let pubkey = Pubkey::new_unique();
        let interfaces: Vec<Interface> = tunnel_endpoint_ips
            .into_iter()
            .enumerate()
            .map(|(i, ip)| {
                Interface::V2(CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: format!("Loopback{}", i),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::None,
                    interface_cyoa: InterfaceCYOA::None,
                    interface_dia: InterfaceDIA::None,
                    bandwidth: 0,
                    cir: 0,
                    mtu: 1500,
                    routing_mode: RoutingMode::Static,
                    vlan_id: 0,
                    ip_net: NetworkV4::new(ip, 32).unwrap(),
                    node_segment_idx: 0,
                    user_tunnel_endpoint: true,
                })
            })
            .collect();
        (
            pubkey,
            Device {
                account_type: AccountType::Device,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                location_pk: Pubkey::default(),
                exchange_pk: Pubkey::default(),
                device_type: DeviceType::Hybrid,
                public_ip,
                status,
                code: "device".to_string(),
                dz_prefixes: NetworkV4List::default(),
                metrics_publisher_pk: Pubkey::default(),
                contributor_pk: Pubkey::default(),
                mgmt_vrf: "default".to_string(),
                interfaces,
                reference_count: 0,
                users_count,
                max_users: 1,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
                unicast_users_count: 0,
                multicast_users_count: 0,
                max_unicast_users: 0,
                max_multicast_users: 0,
            },
        )
    }

    fn make_latency(pk: &str, avg_latency_ns: i32, reachable: bool) -> LatencyRecord {
        LatencyRecord {
            device_pk: pk.to_string(),
            device_code: "device".to_string(),
            device_ip: "0.0.0.0".to_string(),
            min_latency_ns: avg_latency_ns,
            max_latency_ns: avg_latency_ns,
            avg_latency_ns,
            reachable,
        }
    }

    #[tokio::test]
    async fn test_retrieve_latencies_filters_and_sorts() {
        let (pk1, dev1) = make_device(DeviceStatus::Activated, 0);
        let (pk2, dev2) = make_device(DeviceStatus::Activated, 0);
        let (pk3, dev3) = make_device(DeviceStatus::Activated, 0);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);
        devices.insert(pk3, dev3);

        let latencies = vec![
            make_latency(&pk1.to_string(), 10000000, true),
            make_latency(&pk2.to_string(), 20000000, false),
            make_latency(&pk3.to_string(), 5000000, true),
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        let result = retrieve_latencies(&controller, &devices, true, None)
            .await
            .unwrap();
        assert_eq!(result.len(), 2);
        assert_eq!(result[0].device_pk, pk3.to_string());
        assert_eq!(result[1].device_pk, pk1.to_string());

        let result = retrieve_latencies(&controller, &devices, false, None)
            .await
            .unwrap();
        assert_eq!(result.len(), 3);
        assert_eq!(result[0].device_pk, pk3.to_string());
        assert_eq!(result[1].device_pk, pk1.to_string());
        assert_eq!(result[2].device_pk, pk2.to_string());
    }

    #[tokio::test]
    async fn test_best_latency_prefers_current_within_tolerance() {
        let (pk1, dev1) = make_device(DeviceStatus::Activated, 0);
        let (pk2, dev2) = make_device(DeviceStatus::Activated, 0);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);

        let latencies = vec![
            make_latency(&pk1.to_string(), 10000000, true),
            make_latency(&pk2.to_string(), 11000000, true),
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        let result = best_latency(&controller, &devices, true, None, Some(&pk2), &[])
            .await
            .unwrap();

        assert_eq!(result.device_pk, pk2.to_string());
    }

    #[tokio::test]
    async fn test_best_latency_selects_lowest() {
        let (pk1, dev1) = make_device(DeviceStatus::Activated, 0);
        let (pk2, dev2) = make_device(DeviceStatus::Activated, 0);
        let (pk3, dev3) = make_device(DeviceStatus::Activated, 0);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);
        devices.insert(pk3, dev3);

        let latencies = vec![
            make_latency(&pk1.to_string(), 12000000, true),
            make_latency(&pk2.to_string(), 9000000, true),
            make_latency(&pk3.to_string(), 15000000, true),
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        let result = best_latency(&controller, &devices, true, None, None, &[])
            .await
            .unwrap();

        assert_eq!(result.device_pk, pk2.to_string());
    }

    #[tokio::test]
    async fn test_best_latency_ignores_unreachable_devices() {
        let (pk1, dev1) = make_device(DeviceStatus::Activated, 0);
        let (pk2, dev2) = make_device(DeviceStatus::Activated, 0);
        let (pk3, dev3) = make_device(DeviceStatus::Activated, 0);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);
        devices.insert(pk3, dev3);

        let latencies = vec![
            make_latency(&pk1.to_string(), 12000000, false), // unreachable
            make_latency(&pk2.to_string(), 9000000, false),  // unreachable
            make_latency(&pk3.to_string(), 15000000, true),  // reachable
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        let result = best_latency(&controller, &devices, true, None, None, &[])
            .await
            .unwrap();

        assert_eq!(result.device_pk, pk3.to_string());
    }

    #[tokio::test]
    async fn test_best_latency_ignores_faster_devices_at_max_users() {
        let (pk1, dev1) = make_device(DeviceStatus::Activated, 1);
        let (pk2, dev2) = make_device(DeviceStatus::Activated, 0);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);

        let latencies = vec![
            make_latency(&pk1.to_string(), 9000000, true),
            make_latency(&pk2.to_string(), 12000000, true),
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        let result = best_latency(&controller, &devices, true, None, Some(&pk2), &[])
            .await
            .unwrap();

        assert_eq!(result.device_pk, pk2.to_string());
    }

    #[tokio::test]
    async fn test_best_latency_current_faster_but_at_max_users() {
        let (pk1, dev1) = make_device(DeviceStatus::Activated, 0);
        let (pk2, dev2) = make_device(DeviceStatus::Activated, 1);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);

        let latencies = vec![
            make_latency(&pk1.to_string(), 12000000, true),
            make_latency(&pk2.to_string(), 9000000, true),
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        let result = best_latency(&controller, &devices, true, None, Some(&pk2), &[])
            .await
            .unwrap();

        assert_eq!(result.device_pk, pk2.to_string());
    }

    #[tokio::test]
    async fn test_best_latency_excludes_ips() {
        // Create devices with specific public IPs (no additional tunnel endpoints)
        let ip1: Ipv4Addr = "10.0.0.1".parse().unwrap();
        let ip2: Ipv4Addr = "10.0.0.2".parse().unwrap();
        let ip3: Ipv4Addr = "10.0.0.3".parse().unwrap();
        let (pk1, dev1) = make_device_with_ip(DeviceStatus::Activated, 0, ip1, vec![]);
        let (pk2, dev2) = make_device_with_ip(DeviceStatus::Activated, 0, ip2, vec![]);
        let (pk3, dev3) = make_device_with_ip(DeviceStatus::Activated, 0, ip3, vec![]);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);
        devices.insert(pk3, dev3);

        let latencies = vec![
            make_latency(&pk1.to_string(), 12000000, true),
            make_latency(&pk2.to_string(), 5000000, true), // lowest but will be excluded
            make_latency(&pk3.to_string(), 15000000, true),
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        // Exclude all device IPs - all devices should be excluded
        let result = best_latency(&controller, &devices, true, None, None, &[ip1, ip2, ip3]).await;

        // All devices have their only endpoint excluded, so all should be excluded
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_best_latency_excludes_specific_ip() {
        let ip1: Ipv4Addr = "10.0.0.1".parse().unwrap();
        let ip2: Ipv4Addr = "10.0.0.2".parse().unwrap();
        let (pk1, dev1) = make_device_with_ip(DeviceStatus::Activated, 0, ip1, vec![]);
        let (pk2, dev2) = make_device_with_ip(DeviceStatus::Activated, 0, ip2, vec![]);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);

        // Create latencies with different IPs
        let latencies = vec![
            LatencyRecord {
                device_pk: pk1.to_string(),
                device_code: "device1".to_string(),
                device_ip: "10.0.0.1".to_string(),
                min_latency_ns: 5000000,
                max_latency_ns: 5000000,
                avg_latency_ns: 5000000, // lowest latency
                reachable: true,
            },
            LatencyRecord {
                device_pk: pk2.to_string(),
                device_code: "device2".to_string(),
                device_ip: "10.0.0.2".to_string(),
                min_latency_ns: 10000000,
                max_latency_ns: 10000000,
                avg_latency_ns: 10000000,
                reachable: true,
            },
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        // Exclude 10.0.0.1 (pk1's IP), so pk2 should be selected even though it's slower
        let excluded_ip: Ipv4Addr = "10.0.0.1".parse().unwrap();
        let result = best_latency(&controller, &devices, true, None, None, &[excluded_ip])
            .await
            .unwrap();

        assert_eq!(result.device_pk, pk2.to_string());
        assert_eq!(result.device_ip, "10.0.0.2");
    }

    #[tokio::test]
    async fn test_best_latency_device_with_multiple_endpoints_not_excluded() {
        // Device 1 has public_ip AND an additional tunnel endpoint
        let ip1: Ipv4Addr = "10.0.0.1".parse().unwrap();
        let ip1_tunnel: Ipv4Addr = "10.0.0.11".parse().unwrap();
        let ip2: Ipv4Addr = "10.0.0.2".parse().unwrap();
        let (pk1, dev1) = make_device_with_ip(DeviceStatus::Activated, 0, ip1, vec![ip1_tunnel]);
        let (pk2, dev2) = make_device_with_ip(DeviceStatus::Activated, 0, ip2, vec![]);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);

        let latencies = vec![
            LatencyRecord {
                device_pk: pk1.to_string(),
                device_code: "device1".to_string(),
                device_ip: "10.0.0.1".to_string(),
                min_latency_ns: 5000000,
                max_latency_ns: 5000000,
                avg_latency_ns: 5000000, // lowest latency
                reachable: true,
            },
            LatencyRecord {
                device_pk: pk2.to_string(),
                device_code: "device2".to_string(),
                device_ip: "10.0.0.2".to_string(),
                min_latency_ns: 10000000,
                max_latency_ns: 10000000,
                avg_latency_ns: 10000000,
                reachable: true,
            },
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        // Exclude 10.0.0.1 (pk1's public_ip), but pk1 still has 10.0.0.11 available
        // So pk1 should still be selected (lowest latency, still has available endpoint)
        let excluded_ip: Ipv4Addr = "10.0.0.1".parse().unwrap();
        let result = best_latency(&controller, &devices, true, None, None, &[excluded_ip])
            .await
            .unwrap();

        assert_eq!(result.device_pk, pk1.to_string());
    }

    #[tokio::test]
    async fn test_best_latency_device_all_endpoints_excluded() {
        // Device 1 has public_ip AND an additional tunnel endpoint, but both are excluded
        let ip1: Ipv4Addr = "10.0.0.1".parse().unwrap();
        let ip1_tunnel: Ipv4Addr = "10.0.0.11".parse().unwrap();
        let ip2: Ipv4Addr = "10.0.0.2".parse().unwrap();
        let (pk1, dev1) = make_device_with_ip(DeviceStatus::Activated, 0, ip1, vec![ip1_tunnel]);
        let (pk2, dev2) = make_device_with_ip(DeviceStatus::Activated, 0, ip2, vec![]);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);

        let latencies = vec![
            LatencyRecord {
                device_pk: pk1.to_string(),
                device_code: "device1".to_string(),
                device_ip: "10.0.0.1".to_string(),
                min_latency_ns: 5000000,
                max_latency_ns: 5000000,
                avg_latency_ns: 5000000, // lowest latency
                reachable: true,
            },
            LatencyRecord {
                device_pk: pk2.to_string(),
                device_code: "device2".to_string(),
                device_ip: "10.0.0.2".to_string(),
                min_latency_ns: 10000000,
                max_latency_ns: 10000000,
                avg_latency_ns: 10000000,
                reachable: true,
            },
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        // Exclude BOTH of pk1's endpoints (public_ip and tunnel endpoint)
        // Now pk1 should be excluded and pk2 selected
        let result = best_latency(&controller, &devices, true, None, None, &[ip1, ip1_tunnel])
            .await
            .unwrap();

        assert_eq!(result.device_pk, pk2.to_string());
    }

    #[tokio::test]
    async fn test_best_latency_prefers_same_device_with_available_endpoint() {
        // Device 1 (current device) has two tunnel endpoints, one is already in use
        let ip1: Ipv4Addr = "10.0.0.1".parse().unwrap();
        let ip1_tunnel: Ipv4Addr = "10.0.0.11".parse().unwrap();
        let ip2: Ipv4Addr = "10.0.0.2".parse().unwrap();
        let (pk1, dev1) = make_device_with_ip(DeviceStatus::Activated, 0, ip1, vec![ip1_tunnel]);
        let (pk2, dev2) = make_device_with_ip(DeviceStatus::Activated, 0, ip2, vec![]);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);

        let latencies = vec![
            LatencyRecord {
                device_pk: pk1.to_string(),
                device_code: "device1".to_string(),
                device_ip: "10.0.0.1".to_string(),
                min_latency_ns: 10000000,
                max_latency_ns: 10000000,
                avg_latency_ns: 10000000,
                reachable: true,
            },
            LatencyRecord {
                device_pk: pk2.to_string(),
                device_code: "device2".to_string(),
                device_ip: "10.0.0.2".to_string(),
                min_latency_ns: 10000000,
                max_latency_ns: 10000000,
                avg_latency_ns: 10000000, // same latency
                reachable: true,
            },
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        // The public_ip of device 1 is already in use (excluded), but it has another endpoint
        // With current_device set to pk1 and within tolerance, it should still prefer pk1
        let result = best_latency(&controller, &devices, true, None, Some(&pk1), &[ip1])
            .await
            .unwrap();

        // pk1 should still be selected since it has an available endpoint (ip1_tunnel)
        // and is the current device within tolerance
        assert_eq!(result.device_pk, pk1.to_string());
    }

    #[test]
    fn test_get_device_tunnel_endpoints() {
        let ip: Ipv4Addr = "10.0.0.1".parse().unwrap();
        let tunnel1: Ipv4Addr = "10.0.0.11".parse().unwrap();
        let tunnel2: Ipv4Addr = "10.0.0.12".parse().unwrap();
        let (_, device) =
            make_device_with_ip(DeviceStatus::Activated, 0, ip, vec![tunnel1, tunnel2]);

        let endpoints = get_device_tunnel_endpoints(&device);

        assert_eq!(endpoints.len(), 3);
        assert!(endpoints.contains(&ip));
        assert!(endpoints.contains(&tunnel1));
        assert!(endpoints.contains(&tunnel2));
    }

    #[test]
    fn test_get_device_tunnel_endpoints_no_interfaces() {
        let ip: Ipv4Addr = "10.0.0.1".parse().unwrap();
        let (_, device) = make_device_with_ip(DeviceStatus::Activated, 0, ip, vec![]);

        let endpoints = get_device_tunnel_endpoints(&device);

        assert_eq!(endpoints.len(), 1);
        assert!(endpoints.contains(&ip));
    }

    #[test]
    fn test_device_has_available_endpoint() {
        let ip: Ipv4Addr = "10.0.0.1".parse().unwrap();
        let tunnel: Ipv4Addr = "10.0.0.11".parse().unwrap();
        let (_, device) = make_device_with_ip(DeviceStatus::Activated, 0, ip, vec![tunnel]);

        // No exclusions - should have available
        assert!(device_has_available_endpoint(&device, &[]));

        // Only public_ip excluded - tunnel still available
        assert!(device_has_available_endpoint(&device, &[ip]));

        // Only tunnel excluded - public_ip still available
        assert!(device_has_available_endpoint(&device, &[tunnel]));

        // Both excluded - no available endpoints
        assert!(!device_has_available_endpoint(&device, &[ip, tunnel]));
    }

    #[test]
    fn test_select_tunnel_endpoint_picks_lowest_latency() {
        let pk = Pubkey::new_unique();
        let pk_str = pk.to_string();
        let latencies = vec![
            LatencyRecord {
                device_pk: pk_str.clone(),
                device_code: "device1".to_string(),
                device_ip: "10.0.0.1".to_string(),
                min_latency_ns: 20000000,
                max_latency_ns: 20000000,
                avg_latency_ns: 20000000,
                reachable: true,
            },
            LatencyRecord {
                device_pk: pk_str.clone(),
                device_code: "device1".to_string(),
                device_ip: "10.0.0.2".to_string(),
                min_latency_ns: 5000000,
                max_latency_ns: 5000000,
                avg_latency_ns: 5000000,
                reachable: true,
            },
        ];

        let result = select_tunnel_endpoint(&latencies, &pk_str, Ipv4Addr::new(10, 0, 0, 1), &[]);
        assert_eq!(result, Ipv4Addr::new(10, 0, 0, 2));
    }

    #[test]
    fn test_select_tunnel_endpoint_skips_excluded() {
        let pk = Pubkey::new_unique();
        let pk_str = pk.to_string();
        let latencies = vec![
            LatencyRecord {
                device_pk: pk_str.clone(),
                device_code: "device1".to_string(),
                device_ip: "10.0.0.1".to_string(),
                min_latency_ns: 5000000,
                max_latency_ns: 5000000,
                avg_latency_ns: 5000000,
                reachable: true,
            },
            LatencyRecord {
                device_pk: pk_str.clone(),
                device_code: "device1".to_string(),
                device_ip: "10.0.0.2".to_string(),
                min_latency_ns: 10000000,
                max_latency_ns: 10000000,
                avg_latency_ns: 10000000,
                reachable: true,
            },
        ];

        // Exclude the best endpoint, should fall back to the second
        let result = select_tunnel_endpoint(
            &latencies,
            &pk_str,
            Ipv4Addr::new(10, 0, 0, 1),
            &[Ipv4Addr::new(10, 0, 0, 1)],
        );
        assert_eq!(result, Ipv4Addr::new(10, 0, 0, 2));
    }

    #[test]
    fn test_select_tunnel_endpoint_all_excluded_falls_back_to_public_ip() {
        let pk = Pubkey::new_unique();
        let pk_str = pk.to_string();
        let latencies = vec![LatencyRecord {
            device_pk: pk_str.clone(),
            device_code: "device1".to_string(),
            device_ip: "10.0.0.1".to_string(),
            min_latency_ns: 5000000,
            max_latency_ns: 5000000,
            avg_latency_ns: 5000000,
            reachable: true,
        }];

        // Exclude the only latency endpoint, but public_ip is different and available
        let result = select_tunnel_endpoint(
            &latencies,
            &pk_str,
            Ipv4Addr::new(10, 0, 0, 99),
            &[Ipv4Addr::new(10, 0, 0, 1)],
        );
        assert_eq!(result, Ipv4Addr::new(10, 0, 0, 99));
    }

    #[test]
    fn test_select_tunnel_endpoint_all_excluded_returns_unspecified() {
        let pk = Pubkey::new_unique();
        let pk_str = pk.to_string();
        let latencies = vec![LatencyRecord {
            device_pk: pk_str.clone(),
            device_code: "device1".to_string(),
            device_ip: "10.0.0.1".to_string(),
            min_latency_ns: 5000000,
            max_latency_ns: 5000000,
            avg_latency_ns: 5000000,
            reachable: true,
        }];

        // Exclude all endpoints including public_ip
        let result = select_tunnel_endpoint(
            &latencies,
            &pk_str,
            Ipv4Addr::new(10, 0, 0, 1),
            &[Ipv4Addr::new(10, 0, 0, 1)],
        );
        assert_eq!(result, Ipv4Addr::UNSPECIFIED);
    }

    #[test]
    fn test_select_tunnel_endpoint_no_matching_device() {
        let pk = Pubkey::new_unique();
        let other_pk = Pubkey::new_unique();
        let latencies = vec![LatencyRecord {
            device_pk: other_pk.to_string(),
            device_code: "device2".to_string(),
            device_ip: "10.0.0.2".to_string(),
            min_latency_ns: 5000000,
            max_latency_ns: 5000000,
            avg_latency_ns: 5000000,
            reachable: true,
        }];

        // No latency records for this device, should fall back to public_ip
        let result =
            select_tunnel_endpoint(&latencies, &pk.to_string(), Ipv4Addr::new(10, 0, 0, 1), &[]);
        assert_eq!(result, Ipv4Addr::new(10, 0, 0, 1));
    }

    #[test]
    fn test_select_tunnel_endpoint_empty_latencies() {
        let pk = Pubkey::new_unique();
        let result = select_tunnel_endpoint(&[], &pk.to_string(), Ipv4Addr::new(10, 0, 0, 1), &[]);
        // No latency data, fall back to public IP
        assert_eq!(result, Ipv4Addr::new(10, 0, 0, 1));
    }
}
