use crate::ingestor::demand::CityStats;
use anyhow::Result;
use csv::Writer;
use network_shapley::{
    shapley::ShapleyOutput,
    types::{Demand, Device, PrivateLink, PublicLink},
};
use std::{fs::create_dir_all, path::Path};
use tracing::info;

/// Exports all data (except demands) to CSV files in the specified directory
pub fn export_to_csv(
    output_dir: &Path,
    devices: &[Device],
    private_links: &[PrivateLink],
    public_links: &[PublicLink],
    city_stats: &CityStats,
) -> Result<()> {
    // Create dir if it doesn't exist
    create_dir_all(output_dir)?;

    // Write each data type to its own CSV file
    write_devices_csv(output_dir, devices)?;
    write_private_links_csv(output_dir, private_links)?;
    write_public_links_csv(output_dir, public_links)?;
    write_city_stats_csv(output_dir, city_stats)?;

    Ok(())
}

// Export demands with prefix (city)
pub fn write_demands_csv(output_dir: &Path, prefix: &str, demands: &[Demand]) -> Result<()> {
    let path = output_dir.join(format!("demand-{prefix}.csv"));
    let mut writer = Writer::from_path(&path)?;
    writer.write_record([
        "Start",
        "End",
        "Receivers",
        "Traffic",
        "Priority",
        "Type",
        "Multicast",
    ])?;
    for demand in demands {
        writer.write_record([
            &demand.start,
            &demand.end,
            &demand.receivers.to_string(),
            &demand.traffic.to_string(),
            &demand.priority.to_string(),
            &demand.kind.to_string(),
            &demand.multicast.to_string(),
        ])?;
    }
    writer.flush()?;
    info!("Wrote {}", path.display());
    Ok(())
}

// Generic CSV writer trait for reusability (SOLID - Interface Segregation)
pub trait CsvWritable {
    fn write_csv<P: AsRef<Path>>(&self, path: P) -> Result<()>
    where
        Self: serde::Serialize + Sized;
}

// Implement CsvWritable for Vec<T> where T is Serialize
impl<T> CsvWritable for Vec<T>
where
    T: serde::Serialize,
{
    fn write_csv<P: AsRef<Path>>(&self, path: P) -> Result<()> {
        let mut writer = Writer::from_path(path)?;

        for record in self {
            writer.serialize(record)?;
        }

        writer.flush()?;
        Ok(())
    }
}

fn write_devices_csv(output_dir: &Path, devices: &[Device]) -> Result<()> {
    let path = output_dir.join("devices.csv");
    devices.to_vec().write_csv(&path)?;
    info!("Wrote {}", path.display());
    Ok(())
}

fn write_private_links_csv(output_dir: &Path, links: &[PrivateLink]) -> Result<()> {
    let path = output_dir.join("private_links.csv");
    let mut writer = Writer::from_path(&path)?;
    writer.write_record([
        "Device1",
        "Device2",
        "Latency",
        "Bandwidth",
        "Uptime",
        "Shared",
    ])?;
    for link in links {
        writer.write_record([
            &link.device1,
            &link.device2,
            &link.latency.to_string(),
            &link.bandwidth.to_string(),
            &link.uptime.to_string(),
            &link.shared.map_or("NA".to_string(), |v| v.to_string()),
        ])?;
    }
    writer.flush()?;
    info!("Wrote {}", path.display());
    Ok(())
}

/// Write public links
fn write_public_links_csv(output_dir: &Path, links: &[PublicLink]) -> Result<()> {
    let path = output_dir.join("public_links.csv");
    let mut writer = Writer::from_path(&path)?;
    writer.write_record(["City1", "City2", "Latency"])?;
    for link in links {
        writer.write_record([&link.city1, &link.city2, &link.latency.to_string()])?;
    }
    writer.flush()?;
    info!("Wrote {}", path.display());
    Ok(())
}

// Write city stats
fn write_city_stats_csv(output_dir: &Path, city_stats: &CityStats) -> Result<()> {
    let path = output_dir.join("city_stats.csv");
    let mut writer = Writer::from_path(&path)?;
    writer.write_record(["location", "validator_count", "total_stake_proxy"])?;
    for (loc_code, city_stat) in city_stats {
        writer.write_record([
            loc_code,
            &city_stat.validator_count.to_string(),
            &city_stat.total_stake_proxy.to_string(),
        ])?;
    }
    writer.flush()?;
    info!("Wrote {}", path.display());
    Ok(())
}

/// Writes consolidated Shapley output to CSV
pub fn write_consolidated_shapley_csv(
    output_dir: &Path,
    consolidated: &ShapleyOutput,
) -> Result<()> {
    let path = output_dir.join("shapley.csv");
    let mut writer = Writer::from_path(&path)?;
    writer.write_record(["operator", "value", "proportion"])?;
    for (operator, val) in consolidated.iter() {
        writer.write_record([
            operator.to_string(),
            val.value.to_string(),
            val.proportion.to_string(),
        ])?;
    }
    writer.flush()?;
    info!("Wrote {}", path.display());
    Ok(())
}
