use crate::ingestor::demand::CityStats;
use network_shapley::types::{Demand, Device, PrivateLink, PublicLink};
use std::collections::HashMap;
use tabled::{builder::Builder as TableBuilder, settings::Style};

/// Calculate normalized weights for each city based on stake
///
/// # Arguments
/// * `city_stats` - Map of city to CityStat containing stake information
///
/// # Returns
/// HashMap mapping city names to their normalized weights (0.0 to 1.0, sum = 1.0)
pub fn calculate_city_weights(city_stats: &CityStats) -> HashMap<String, f64> {
    // Calculate total stake across all cities
    let total_stake: f64 = city_stats
        .values()
        .map(|stat| stat.total_stake_proxy as f64)
        .sum();

    // Calculate normalized weights for each city
    city_stats
        .iter()
        .map(|(city, stat)| {
            let weight = if total_stake > 0.0 {
                stat.total_stake_proxy as f64 / total_stake
            } else {
                // If no stake, use equal weights
                1.0 / city_stats.len() as f64
            };
            (city.clone(), weight)
        })
        .collect()
}

pub fn print_devices(devices: &[Device]) -> String {
    let mut printable = vec![vec![
        "device".to_string(),
        "bandwidth(Gbps)".to_string(),
        "operator".to_string(),
    ]];

    for dev in devices {
        let row = vec![
            dev.device.to_string(),
            dev.edge.to_string(), // aka bandwidth (Gbps)
            dev.operator.to_string(),
        ];
        printable.push(row);
    }

    TableBuilder::from(printable)
        .build()
        .with(Style::psql().remove_horizontals())
        .to_string()
}

pub fn print_public_links(public_links: &[PublicLink]) -> String {
    let mut printable = vec![vec![
        "city1".to_string(),
        "city2".to_string(),
        "latency(ms)".to_string(),
    ]];

    for link in public_links {
        let row = vec![
            link.city1.to_string(),
            link.city2.to_string(),
            link.latency.to_string(),
        ];
        printable.push(row);
    }

    TableBuilder::from(printable)
        .build()
        .with(Style::psql().remove_horizontals())
        .to_string()
}

pub fn print_private_links(private_links: &[PrivateLink]) -> String {
    let mut printable = vec![vec![
        "device1".to_string(),
        "device2".to_string(),
        "latency(ms)".to_string(),
        "bandwidth(Gbps)".to_string(),
        "uptime".to_string(),
        "shared".to_string(),
    ]];

    for pl in private_links {
        let row = vec![
            pl.device1.to_string(),
            pl.device2.to_string(),
            pl.latency.to_string(),
            pl.bandwidth.to_string(),
            pl.uptime.to_string(),
            format!("{:?}", pl.shared),
        ];
        printable.push(row);
    }

    TableBuilder::from(printable)
        .build()
        .with(Style::psql().remove_horizontals())
        .to_string()
}

pub fn print_demands(demands: &[Demand], k: usize) -> String {
    let mut printable = vec![vec![
        "start".to_string(),
        "end".to_string(),
        "receivers".to_string(),
        "traffic".to_string(),
        "priority".to_string(),
        "type".to_string(),
        "multicast".to_string(),
    ]];

    for demand in demands.iter().take(k) {
        let row = vec![
            demand.start.to_string(),
            demand.end.to_string(),
            demand.receivers.to_string(),
            demand.traffic.to_string(),
            demand.priority.to_string(),
            demand.kind.to_string(),
            demand.multicast.to_string(),
        ];
        printable.push(row);
    }

    TableBuilder::from(printable)
        .build()
        .with(Style::psql().remove_horizontals())
        .to_string()
}
