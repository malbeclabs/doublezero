use crate::servicecontroller::{LatencyRecord, ServiceController};
use backon::{ExponentialBuilder, Retryable};
use doublezero_sdk::{Device, DeviceStatus};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, net::Ipv4Addr, str::FromStr, time::Duration};

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

<<<<<<< HEAD
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
=======
>>>>>>> 42711d2f (DNM: feat(cli): remove multiple tunnel restriction (#2725))
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

    if !exclude_ips.is_empty() {
        latencies.retain(|l| {
            l.device_ip
                .parse::<Ipv4Addr>()
                .map(|ip| !exclude_ips.contains(&ip))
                .unwrap_or(true)
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
    use doublezero_program_common::types::NetworkV4List;
    use doublezero_sdk::{AccountType, Device, DeviceStatus, DeviceType};
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    fn make_device(status: DeviceStatus, users_count: u16) -> (Pubkey, Device) {
        let pubkey = Pubkey::new_unique();
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
                public_ip: std::net::Ipv4Addr::UNSPECIFIED,
                status,
                code: "device".to_string(),
                dz_prefixes: NetworkV4List::default(),
                metrics_publisher_pk: Pubkey::default(),
                contributor_pk: Pubkey::default(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                reference_count: 0,
                users_count,
                max_users: 1,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
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
        let (pk1, dev1) = make_device(DeviceStatus::Activated, 0);
        let (pk2, dev2) = make_device(DeviceStatus::Activated, 0);
        let (pk3, dev3) = make_device(DeviceStatus::Activated, 0);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);
        devices.insert(pk3, dev3);

        // pk2 has the lowest latency but its IP will be excluded
        let latencies = vec![
            make_latency(&pk1.to_string(), 12000000, true),
            make_latency(&pk2.to_string(), 5000000, true), // lowest but excluded
            make_latency(&pk3.to_string(), 15000000, true),
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        // Exclude the IP "0.0.0.0" which is used by make_latency
        let excluded_ip: Ipv4Addr = "0.0.0.0".parse().unwrap();
        let result = best_latency(&controller, &devices, true, None, None, &[excluded_ip]).await;

        // All devices have IP 0.0.0.0, so all should be excluded
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_best_latency_excludes_specific_ip() {
        let (pk1, dev1) = make_device(DeviceStatus::Activated, 0);
        let (pk2, dev2) = make_device(DeviceStatus::Activated, 0);

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
}
