use crate::servicecontroller::{LatencyRecord, ServiceController};
use backon::{ExponentialBuilder, Retryable};
use doublezero_sdk::{Device, DeviceStatus};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, str::FromStr, time::Duration};

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

const LATENCY_TOLERANCE_NS: i32 = 1_500_000; // 1.5 ms

/// Finds the best (lowest latency) record for a specific device among all its IPs.
/// When a device has multiple IPs (public IP and interface IPs), this returns the
/// record with the lowest avg_latency_ns.
fn best_latency_for_device<'a>(
    latencies: &'a [LatencyRecord],
    device_pk: &Pubkey,
) -> Option<&'a LatencyRecord> {
    let device_pk_str = device_pk.to_string();
    latencies
        .iter()
        .filter(|l| l.device_pk == device_pk_str)
        .min_by_key(|l| l.avg_latency_ns)
}

pub async fn best_latency<T: ServiceController>(
    controller: &T,
    devices: &HashMap<Pubkey, Device>,
    ignore_unprovisionable: bool,
    spinner: Option<&indicatif::ProgressBar>,
    current_device: Option<&Pubkey>,
) -> eyre::Result<LatencyRecord> {
    let latencies = retrieve_latencies(controller, devices, true, spinner).await?;
    let mut best: Option<&LatencyRecord> = None;
    let mut best_latency_ns = i32::MAX;

    // If we have a current device, find its best IP (lowest latency among all its IPs)
    if let Some(current_device) = current_device {
        if let Some(current) = best_latency_for_device(&latencies, current_device) {
            best = Some(current);
            best_latency_ns = current.avg_latency_ns;
        }
    }

    // Track which devices we've already considered (to handle multi-IP devices)
    let mut seen_devices = std::collections::HashSet::new();

    for latency in &latencies {
        let device_pk = Pubkey::from_str(&latency.device_pk)?;

        // Skip if we've already processed this device
        if !seen_devices.insert(device_pk) {
            continue;
        }

        // Get the best latency for this device (in case it has multiple IPs)
        let device_best = best_latency_for_device(&latencies, &device_pk)
            .expect("device must have at least one latency record");

        // If this is our current device's best record, return it
        if let Some(current) = best {
            if std::ptr::eq(current, device_best) {
                return Ok(current.clone());
            }
        }

        let device = devices
            .get(&device_pk)
            .ok_or_else(|| eyre::eyre!("Device with pubkey {} not found", &latency.device_pk))?;

        if (!ignore_unprovisionable || device.is_device_eligible_for_provisioning())
            && (device_best.avg_latency_ns - best_latency_ns).abs() > LATENCY_TOLERANCE_NS
        {
            best = Some(device_best);
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
        make_latency_with_ip(pk, avg_latency_ns, reachable, "0.0.0.0")
    }

    fn make_latency_with_ip(
        pk: &str,
        avg_latency_ns: i32,
        reachable: bool,
        ip: &str,
    ) -> LatencyRecord {
        LatencyRecord {
            device_pk: pk.to_string(),
            device_code: "device".to_string(),
            device_ip: ip.to_string(),
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

        let result = best_latency(&controller, &devices, true, None, Some(&pk2))
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

        let result = best_latency(&controller, &devices, true, None, None)
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

        let result = best_latency(&controller, &devices, true, None, None)
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

        let result = best_latency(&controller, &devices, true, None, Some(&pk2))
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

        let result = best_latency(&controller, &devices, true, None, Some(&pk2))
            .await
            .unwrap();

        assert_eq!(result.device_pk, pk2.to_string());
    }

    #[tokio::test]
    async fn test_best_latency_multi_ip_selects_best_ip_for_device() {
        // Device has multiple IPs with different latencies
        let (pk1, dev1) = make_device(DeviceStatus::Activated, 0);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);

        // Same device has 3 IPs with different latencies
        let latencies = vec![
            make_latency_with_ip(&pk1.to_string(), 15000000, true, "10.0.0.1"), // slower
            make_latency_with_ip(&pk1.to_string(), 8000000, true, "10.0.0.2"),  // fastest
            make_latency_with_ip(&pk1.to_string(), 12000000, true, "10.0.0.3"), // medium
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        let result = best_latency(&controller, &devices, true, None, None)
            .await
            .unwrap();

        // Should select the IP with the best latency
        assert_eq!(result.device_pk, pk1.to_string());
        assert_eq!(result.device_ip, "10.0.0.2");
        assert_eq!(result.avg_latency_ns, 8000000);
    }

    #[tokio::test]
    async fn test_best_latency_multi_ip_current_device_uses_best_ip() {
        // Current device has multiple IPs, should use the best one for comparison
        let (pk1, dev1) = make_device(DeviceStatus::Activated, 0);
        let (pk2, dev2) = make_device(DeviceStatus::Activated, 0);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);

        // pk1 (current device) has multiple IPs, pk2 has one IP
        let latencies = vec![
            make_latency_with_ip(&pk1.to_string(), 15000000, true, "10.0.0.1"), // slower IP
            make_latency_with_ip(&pk1.to_string(), 10000000, true, "10.0.0.2"), // faster IP
            make_latency_with_ip(&pk2.to_string(), 11000000, true, "10.0.1.1"), // within tolerance of pk1's best
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        // With pk1 as current device, should stick with it since pk2 is within tolerance
        let result = best_latency(&controller, &devices, true, None, Some(&pk1))
            .await
            .unwrap();

        assert_eq!(result.device_pk, pk1.to_string());
        // Should return the best IP for the device
        assert_eq!(result.device_ip, "10.0.0.2");
        assert_eq!(result.avg_latency_ns, 10000000);
    }

    #[tokio::test]
    async fn test_best_latency_multi_ip_switches_when_better_device_exists() {
        // Current device has multiple IPs, but another device is significantly better
        let (pk1, dev1) = make_device(DeviceStatus::Activated, 0);
        let (pk2, dev2) = make_device(DeviceStatus::Activated, 0);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);

        // pk1 (current) has multiple IPs all slower, pk2 is much faster
        let latencies = vec![
            make_latency_with_ip(&pk1.to_string(), 20000000, true, "10.0.0.1"),
            make_latency_with_ip(&pk1.to_string(), 18000000, true, "10.0.0.2"), // pk1's best
            make_latency_with_ip(&pk2.to_string(), 5000000, true, "10.0.1.1"),  // much faster
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        // Should switch to pk2 because it's significantly better
        let result = best_latency(&controller, &devices, true, None, Some(&pk1))
            .await
            .unwrap();

        assert_eq!(result.device_pk, pk2.to_string());
        assert_eq!(result.device_ip, "10.0.1.1");
    }

    #[tokio::test]
    async fn test_best_latency_multi_ip_multiple_devices() {
        // Multiple devices each with multiple IPs
        let (pk1, dev1) = make_device(DeviceStatus::Activated, 0);
        let (pk2, dev2) = make_device(DeviceStatus::Activated, 0);
        let (pk3, dev3) = make_device(DeviceStatus::Activated, 0);

        let mut devices = HashMap::new();
        devices.insert(pk1, dev1);
        devices.insert(pk2, dev2);
        devices.insert(pk3, dev3);

        let latencies = vec![
            // pk1: best is 12ms
            make_latency_with_ip(&pk1.to_string(), 15000000, true, "10.0.0.1"),
            make_latency_with_ip(&pk1.to_string(), 12000000, true, "10.0.0.2"),
            // pk2: best is 8ms (overall best)
            make_latency_with_ip(&pk2.to_string(), 10000000, true, "10.0.1.1"),
            make_latency_with_ip(&pk2.to_string(), 8000000, true, "10.0.1.2"),
            // pk3: best is 14ms
            make_latency_with_ip(&pk3.to_string(), 14000000, true, "10.0.2.1"),
            make_latency_with_ip(&pk3.to_string(), 16000000, true, "10.0.2.2"),
        ];

        let mut controller = MockServiceController::new();
        controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));

        let result = best_latency(&controller, &devices, true, None, None)
            .await
            .unwrap();

        // Should select pk2's best IP
        assert_eq!(result.device_pk, pk2.to_string());
        assert_eq!(result.device_ip, "10.0.1.2");
        assert_eq!(result.avg_latency_ns, 8000000);
    }

    #[test]
    fn test_best_latency_for_device_helper() {
        let pk1 = Pubkey::new_unique();
        let pk2 = Pubkey::new_unique();

        let latencies = vec![
            make_latency_with_ip(&pk1.to_string(), 15000000, true, "10.0.0.1"),
            make_latency_with_ip(&pk1.to_string(), 8000000, true, "10.0.0.2"),
            make_latency_with_ip(&pk1.to_string(), 12000000, true, "10.0.0.3"),
            make_latency_with_ip(&pk2.to_string(), 5000000, true, "10.0.1.1"),
        ];

        // Should find the best (lowest latency) for pk1
        let result = best_latency_for_device(&latencies, &pk1);
        assert!(result.is_some());
        let record = result.unwrap();
        assert_eq!(record.device_ip, "10.0.0.2");
        assert_eq!(record.avg_latency_ns, 8000000);

        // Should find the only one for pk2
        let result = best_latency_for_device(&latencies, &pk2);
        assert!(result.is_some());
        let record = result.unwrap();
        assert_eq!(record.device_ip, "10.0.1.1");
        assert_eq!(record.avg_latency_ns, 5000000);

        // Should return None for unknown device
        let pk3 = Pubkey::new_unique();
        let result = best_latency_for_device(&latencies, &pk3);
        assert!(result.is_none());
    }
}
