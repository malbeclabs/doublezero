use crate::cli::traits::Exportable;
use anyhow::Result;
use clap::{Args, ValueEnum};
use serde::{Deserialize, Serialize};
use std::{
    fmt,
    fs::{File, create_dir_all},
    io::Write,
    path::Path,
};
use tracing::info;

/// Unified output format for all CLI commands
#[derive(Debug, Clone, Copy, ValueEnum, Serialize, Deserialize)]
pub enum OutputFormat {
    #[value(name = "csv")]
    Csv,
    #[value(name = "json")]
    Json,
    #[value(name = "json-pretty")]
    JsonPretty,
}

impl fmt::Display for OutputFormat {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Csv => write!(f, "csv"),
            Self::Json => write!(f, "json"),
            Self::JsonPretty => write!(f, "json-pretty"),
        }
    }
}

/// Common output options for CLI commands
#[derive(Args, Debug, Clone)]
pub struct OutputOptions {
    /// Output format for exports
    #[arg(short = 'f', long, default_value = "json-pretty")]
    pub output_format: OutputFormat,

    /// Directory to export files
    #[arg(short = 'o', long, value_name = "DIR")]
    pub output_dir: Option<String>,

    /// Specific output file path
    #[arg(long, value_name = "FILE")]
    pub output_file: Option<String>,
}

impl OutputOptions {
    /// Write exportable data to file or stdout
    pub fn write<T: Exportable>(&self, data: &T, default_filename: &str) -> Result<()> {
        let content = data.export(self.output_format)?;

        if let Some(ref file_path) = self.output_file {
            // Write to specific file
            let path = Path::new(file_path);
            if let Some(parent) = path.parent() {
                create_dir_all(parent)?;
            }
            let mut file = File::create(path)?;
            file.write_all(content.as_bytes())?;
            info!("Exported to: {}", path.display());
        } else if let Some(ref dir) = self.output_dir {
            // Write to directory with default filename
            let dir_path = Path::new(dir);
            create_dir_all(dir_path)?;

            let extension = match self.output_format {
                OutputFormat::Csv => "csv",
                OutputFormat::Json | OutputFormat::JsonPretty => "json",
            };

            let filename = format!("{default_filename}.{extension}");
            let file_path = dir_path.join(filename);

            let mut file = File::create(&file_path)?;
            file.write_all(content.as_bytes())?;
            info!("Exported to: {}", file_path.display());
        } else {
            // Write to stdout
            println!("{content}");
        }

        Ok(())
    }
}

/// Common filter options for telemetry and other data
#[derive(Args, Debug, Clone)]
pub struct FilterOptions {
    /// Filter by origin city/location
    #[arg(long, value_name = "CITY")]
    pub from_city: Option<String>,

    /// Filter by destination city/location
    #[arg(long, value_name = "CITY")]
    pub to_city: Option<String>,

    /// Filter by city (for single location filtering)
    #[arg(long, value_name = "CITY")]
    pub city: Option<String>,

    /// Filter by device ID
    #[arg(long, value_name = "ID")]
    pub device: Option<String>,

    /// Filter by operator/contributor
    #[arg(long, value_name = "PUBKEY")]
    pub operator: Option<String>,

    /// Maximum number of results to return
    #[arg(long, value_name = "NUM")]
    pub limit: Option<usize>,
}

/// Common threshold options for analysis
#[derive(Args, Debug, Clone)]
pub struct ThresholdOptions {
    /// Latency threshold in milliseconds
    #[arg(long, value_name = "MS")]
    pub threshold_ms: Option<f64>,

    /// Minimum packet loss percentage (0.0-1.0)
    #[arg(long, value_name = "PERCENT")]
    pub min_packet_loss: Option<f64>,

    /// Minimum jitter in milliseconds
    #[arg(long, value_name = "MS")]
    pub min_jitter: Option<f64>,

    /// Minimum uptime percentage (0.0-1.0)
    #[arg(long, value_name = "PERCENT")]
    pub min_uptime: Option<f64>,
}

/// Helper function to convert a collection to CSV format
pub fn collection_to_csv<T: Serialize>(records: &[T]) -> Result<String> {
    let mut wtr = csv::Writer::from_writer(vec![]);
    for record in records {
        wtr.serialize(record)?;
    }
    let data = wtr.into_inner()?;
    Ok(String::from_utf8(data)?)
}

/// Helper function to convert data to JSON format
pub fn to_json_string<T: Serialize>(data: &T, pretty: bool) -> Result<String> {
    if pretty {
        Ok(serde_json::to_string_pretty(data)?)
    } else {
        Ok(serde_json::to_string(data)?)
    }
}
