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

pub async fn best_latency<T: ServiceController>(
    controller: &T,
    devices: &HashMap<Pubkey, Device>,
    ignore_unprovisionable: bool,
    spinner: Option<&indicatif::ProgressBar>,
    current_device: Option<&Pubkey>,
) -> eyre::Result<LatencyRecord> {
    let latencies = retrieve_latencies(controller, devices, true, spinner).await?;
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
}
