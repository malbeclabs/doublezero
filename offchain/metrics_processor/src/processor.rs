use crate::{
    data_store::{DataStore, ProcessedMetrics},
    shapley_types::ShapleyInputs,
    telemetry_processor::{TelemetryProcessor, TelemetryStats},
};
use anyhow::Result;
use network_shapley::types::{Demand, PrivateLink, PublicLink};
use rust_decimal::{Decimal, prelude::ToPrimitive};
use std::collections::{HashMap, HashSet};
use tracing::info;

pub struct MetricsProcessorV2 {
    data_store: DataStore,
    after_us: u64,
    before_us: u64,
}

impl MetricsProcessorV2 {
    pub fn new(data_store: DataStore) -> Self {
        let after_us = data_store.metadata.after_us;
        let before_us = data_store.metadata.before_us;

        Self {
            data_store,
            after_us,
            before_us,
        }
    }

    pub fn from_raw(data_store: DataStore, after_us: u64, before_us: u64) -> Self {
        Self {
            data_store,
            after_us,
            before_us,
        }
    }

    pub fn get_data_store(&self) -> &DataStore {
        &self.data_store
    }

    pub fn process_metrics(&self) -> Result<(ShapleyInputs, ProcessedMetrics)> {
        info!("Processing metrics for Shapley calculation (in-memory)");

        let device_to_location_map = self.get_device_to_location_map();
        let device_to_operator = self.get_device_to_operator_map();

        let telemetry_stats = TelemetryProcessor::calculate_all_stats(&self.data_store);
        info!(
            "Calculated telemetry stats for {} links",
            telemetry_stats.len()
        );

        let private_links = self.process_private_links(&telemetry_stats)?;
        info!("Processed {} private links", private_links.len());

        let mut all_private_switches = HashSet::new();
        for link in &private_links {
            all_private_switches.insert(link.device1.clone());
            all_private_switches.insert(link.device2.clone());
        }

        let public_links = self
            .generate_public_links_for_switches(&all_private_switches, &device_to_location_map)?;
        info!("Generated {} public links", public_links.len());

        let mut demand_matrix = self.calculate_demand_matrix()?;
        info!(
            "Calculated {} device-level demand entries",
            demand_matrix.len()
        );

        for demand in &mut demand_matrix {
            demand.start = device_to_location_map
                .get(&demand.start)
                .cloned()
                .unwrap_or_default();
            demand.end = device_to_location_map
                .get(&demand.end)
                .cloned()
                .unwrap_or_default();
        }

        let processed_metrics = ProcessedMetrics {
            private_links_count: private_links.len(),
            public_links_count: public_links.len(),
            demand_entries_count: demand_matrix.len(),
            telemetry_stats_count: telemetry_stats.len(),
        };

        let shapley_inputs = ShapleyInputs {
            private_links,
            public_links,
            demand_matrix,
            demand_multiplier: Decimal::from_str_exact("1.2")?,
            device_to_operator,
        };

        Ok((shapley_inputs, processed_metrics))
    }

    pub fn get_device_to_location_map(&self) -> HashMap<String, String> {
        self.data_store
            .devices
            .values()
            .filter(|d| d.status == "activated")
            .map(|device| {
                let location_code = device
                    .location_pubkey
                    .as_ref()
                    .and_then(|pk| self.data_store.locations.get(pk))
                    .map(|loc| loc.code.clone())
                    .unwrap_or_else(|| "UNK".to_string());
                (device.code.clone(), location_code)
            })
            .collect()
    }

    pub fn get_device_to_operator_map(&self) -> HashMap<String, String> {
        self.data_store
            .devices
            .values()
            .filter(|d| d.status == "activated")
            .map(|device| (device.code.clone(), device.owner.clone()))
            .collect()
    }

    pub fn process_private_links(
        &self,
        telemetry_stats: &HashMap<String, TelemetryStats>,
    ) -> Result<Vec<PrivateLink>> {
        let mut private_links = Vec::new();

        for link in self.data_store.links.values() {
            if link.link_type != "private" || link.status != "active" {
                continue;
            }

            let (from_device, to_device) = self.data_store.get_link_devices(link);

            let (from_device, to_device) = match (from_device, to_device) {
                (Some(f), Some(t)) if f.status == "activated" && t.status == "activated" => (f, t),
                _ => continue,
            };

            let from_location = self.data_store.get_device_location(&from_device.pubkey);
            let to_location = self.data_store.get_device_location(&to_device.pubkey);

            let (_from_location, _to_location) = match (from_location, to_location) {
                (Some(f), Some(t)) => (f, t),
                _ => continue,
            };

            let _operator = if from_device.owner == to_device.owner {
                from_device.owner.clone()
            } else {
                "0".to_string()
            };

            let bandwidth_mbps = (link.bandwidth / 1_000_000) as f64;

            let (latency_ms, _jitter_ms, _packet_loss) =
                if let Some(stats) = telemetry_stats.get(&link.pubkey) {
                    (
                        stats.mean_latency_ms,
                        stats.avg_jitter_ms,
                        stats.packet_loss,
                    )
                } else {
                    (10.0, 2.0, 0.0001)
                };

            let utilization = telemetry_stats
                .get(&link.pubkey)
                .map(|stats| {
                    let sample_rate = if self.before_us > self.after_us {
                        (stats.total_samples as f64)
                            / ((self.before_us - self.after_us) as f64 / 1_000_000.0)
                    } else {
                        0.0
                    };
                    (sample_rate / 10.0).clamp(0.5, 1.0)
                })
                .unwrap_or(0.5);

            // For now, just use latency as the cost (similar to network-shapley examples)
            // TODO: Add more sophisticated cost calculation if needed
            let cost = latency_ms;

            private_links.push(PrivateLink::new(
                from_device.code.clone(),
                to_device.code.clone(),
                cost,
                bandwidth_mbps,
                utilization,
                None,
            ));
        }

        Ok(private_links)
    }

    pub fn generate_public_links_for_switches(
        &self,
        private_switches: &HashSet<String>,
        device_to_location_map: &HashMap<String, String>,
    ) -> Result<Vec<PublicLink>> {
        let mut public_links = Vec::new();
        let switches: Vec<_> = private_switches.iter().collect();

        for i in 0..switches.len() {
            for j in i + 1..switches.len() {
                let device1 = switches[i];
                let device2 = switches[j];

                let loc1 = device_to_location_map
                    .get(device1)
                    .map(|s| s.as_str())
                    .unwrap_or("UNK");
                let loc2 = device_to_location_map
                    .get(device2)
                    .map(|s| s.as_str())
                    .unwrap_or("UNK");

                let baseline = self.find_or_generate_baseline(loc1, loc2);

                let _bandwidth_mbps = baseline.bandwidth_mbps.to_f64().unwrap_or(10.0);
                let latency_ms = baseline.latency_ms.to_f64().unwrap_or(100.0);
                let _jitter_ms = baseline.jitter_ms.to_f64().unwrap_or(20.0);
                let _packet_loss = baseline.packet_loss.to_f64().unwrap_or(0.01);

                // For public links, just use latency as cost
                let cost = latency_ms;

                // PublicLink uses city names, not device names
                public_links.push(PublicLink::new(loc1.to_string(), loc2.to_string(), cost));
            }
        }

        Ok(public_links)
    }

    pub fn find_or_generate_baseline(
        &self,
        loc1: &str,
        loc2: &str,
    ) -> crate::data_store::InternetBaseline {
        let (from_loc, to_loc) = if loc1 < loc2 {
            (loc1, loc2)
        } else {
            (loc2, loc1)
        };

        for baseline in &self.data_store.internet_baselines {
            if (baseline.from_location_code == from_loc && baseline.to_location_code == to_loc)
                || (baseline.from_location_code == to_loc && baseline.to_location_code == from_loc)
            {
                return baseline.clone();
            }
        }

        let loc1_data = self.data_store.get_location_by_code(loc1);
        let loc2_data = self.data_store.get_location_by_code(loc2);

        let (lat1, lng1) = loc1_data.map(|l| (l.lat, l.lng)).unwrap_or((0.0, 0.0));
        let (lat2, lng2) = loc2_data.map(|l| (l.lat, l.lng)).unwrap_or((0.0, 0.0));

        let distance_km = haversine_distance(lat1, lng1, lat2, lng2);
        let latency_ms = (distance_km * 0.01).clamp(5.0, 300.0);
        let jitter_ms = latency_ms * 0.2;
        let packet_loss = 0.001 * (1.0 + distance_km / 10000.0);
        let bandwidth_mbps = 100.0 / (1.0 + distance_km / 5000.0);

        crate::data_store::InternetBaseline {
            from_location_code: from_loc.to_string(),
            to_location_code: to_loc.to_string(),
            from_lat: lat1,
            from_lng: lng1,
            to_lat: lat2,
            to_lng: lng2,
            distance_km: Decimal::from_f64_retain(distance_km).unwrap_or_default(),
            latency_ms: Decimal::from_f64_retain(latency_ms).unwrap_or_default(),
            jitter_ms: Decimal::from_f64_retain(jitter_ms).unwrap_or_default(),
            packet_loss: Decimal::from_f64_retain(packet_loss).unwrap_or_default(),
            bandwidth_mbps: Decimal::from_f64_retain(bandwidth_mbps).unwrap_or_default(),
        }
    }

    pub fn calculate_demand_matrix(&self) -> Result<Vec<Demand>> {
        let mut demand_map: HashMap<(String, String), f64> = HashMap::new();

        for user in self.data_store.users.values() {
            if user.status != "activated" {
                continue;
            }

            if let Some(device_pk) = &user.device_pk {
                if let Some(device) = self.data_store.devices.get(device_pk) {
                    if device.status == "activated" {
                        let device_code = device.code.clone();

                        for publisher_pk in &user.publishers {
                            if let Some(pub_device) = self.data_store.devices.get(publisher_pk) {
                                if pub_device.status == "activated" {
                                    let key = (pub_device.code.clone(), device_code.clone());
                                    *demand_map.entry(key).or_insert(0.0) += 0.5;
                                }
                            }
                        }

                        for subscriber_pk in &user.subscribers {
                            if let Some(sub_device) = self.data_store.devices.get(subscriber_pk) {
                                if sub_device.status == "activated" {
                                    let key = (device_code.clone(), sub_device.code.clone());
                                    *demand_map.entry(key).or_insert(0.0) += 0.5;
                                }
                            }
                        }
                    }
                }
            }
        }

        let demands: Vec<Demand> = demand_map
            .into_iter()
            .map(|((start, end), traffic)| {
                Demand::new(
                    start,
                    end,
                    1, // receivers
                    traffic.max(0.1),
                    1.0,   // priority
                    1,     // kind/type
                    false, // multicast
                )
            })
            .collect();

        Ok(demands)
    }
}

pub fn haversine_distance(lat1: f64, lng1: f64, lat2: f64, lng2: f64) -> f64 {
    const EARTH_RADIUS_KM: f64 = 6371.0;

    let lat1_rad = lat1.to_radians();
    let lat2_rad = lat2.to_radians();
    let delta_lat = (lat2 - lat1).to_radians();
    let delta_lng = (lng2 - lng1).to_radians();

    let a = (delta_lat / 2.0).sin().powi(2)
        + lat1_rad.cos() * lat2_rad.cos() * (delta_lng / 2.0).sin().powi(2);
    let c = 2.0 * a.sqrt().atan2((1.0 - a).sqrt());

    EARTH_RADIUS_KM * c
}
