use crate::servicecontroller::{LatencyRecord, ServiceController};
use backon::{ExponentialBuilder, Retryable};
use doublezero_sdk::{Device, DeviceStatus};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;
use std::{str::FromStr, time::Duration};

pub async fn retrieve_latencies<T: ServiceController>(
    controller: &T,
    devices: &HashMap<Pubkey, Device>,
    reachable_only: bool,
    spinner: Option<&indicatif::ProgressBar>,
) -> eyre::Result<Vec<LatencyRecord>> {
    spinner.map(|s| s.set_message("Reading latency stats..."));

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
            spinner.map(|s| s.set_message(format!("Waiting for latency stats after {dur:?}")));
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
    reachable_only: bool,
    spinner: Option<&indicatif::ProgressBar>,
    current_device: Option<&Pubkey>,
) -> eyre::Result<LatencyRecord> {
    let latencies = retrieve_latencies(controller, devices, reachable_only, spinner).await?;
    let best_device = latencies.first().unwrap();
    if let Some(current_device) = current_device {
        let pk = current_device.to_string();
        if pk == best_device.device_pk {
            return Ok(best_device.clone());
        }
        for latency in &latencies[1..] {
            if latency.device_pk == pk {
                if (latency.avg_latency_ns - best_device.avg_latency_ns).abs()
                    <= LATENCY_TOLERANCE_NS
                {
                    return Ok(latency.clone());
                }
                return Ok(best_device.clone());
            }
        }
    }
    Ok(best_device.clone())
}
