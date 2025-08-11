use anyhow::Result;
use csv::Writer;
use network_shapley::types::{Demand, Device, PrivateLink, PublicLink};
use std::{fs::create_dir_all, path::Path};
use tracing::info;

/// Exports all data (except demands) to CSV files in the specified directory
pub fn export_to_csv(
    output_dir: &Path,
    devices: &[Device],
    private_links: &[PrivateLink],
    public_links: &[PublicLink],
) -> Result<()> {
    // Create dir if it doesn't exist
    create_dir_all(output_dir)?;

    // Write each data type to its own CSV file
    write_devices_csv(output_dir, devices)?;
    write_private_links_csv(output_dir, private_links)?;
    write_public_links_csv(output_dir, public_links)?;

    Ok(())
}

// Export demands with prefix (city)
pub fn write_demands_csv(output_dir: &Path, prefix: &str, demands: &[Demand]) -> Result<()> {
    let path = output_dir.join(format!("demand-{prefix}.csv"));

    info!("Writing {}", path.display());
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
    devices.to_vec().write_csv(path)?;
    Ok(())
}

fn write_private_links_csv(output_dir: &Path, links: &[PrivateLink]) -> Result<()> {
    let path = output_dir.join("private_links.csv");
    let mut writer = Writer::from_path(path)?;
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
    Ok(())
}

fn write_public_links_csv(output_dir: &Path, links: &[PublicLink]) -> Result<()> {
    let path = output_dir.join("public_links.csv");
    let mut writer = Writer::from_path(path)?;
    writer.write_record(["City1", "City2", "Latency"])?;
    for link in links {
        writer.write_record([&link.city1, &link.city2, &link.latency.to_string()])?;
    }
    writer.flush()?;
    Ok(())
}
