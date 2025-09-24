use crate::{
    calculator::orchestrator::Orchestrator,
    cli::{
        common::{
            FilterOptions, OutputFormat, OutputOptions, ThresholdOptions, collection_to_csv,
            to_json_string,
        },
        traits::Exportable,
    },
    ingestor::{
        fetcher::Fetcher,
        types::{DZDeviceLatencySamples, DZInternetLatencySamples, KeyedAccounts},
    },
    processor::{
        internet::{InternetTelemetryProcessor, InternetTelemetryStats},
        telemetry::{DZDTelemetryProcessor, DZDTelemetryStats},
    },
};
use anyhow::{Result, bail};
use clap::Subcommand;
use serde::{Deserialize, Serialize};
use std::{collections::BTreeMap, str::FromStr};
use tabled::{Table, Tabled, settings::Style};
use tracing::info;

/// Telemetry type selection
#[derive(Debug, Clone, Copy)]
pub enum TelemetryType {
    Internet,
    Device,
    All,
}

impl FromStr for TelemetryType {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "internet" | "i" => Ok(TelemetryType::Internet),
            "device" | "d" => Ok(TelemetryType::Device),
            "both" | "all" | "a" => Ok(TelemetryType::All),
            _ => Err(format!(
                "Invalid telemetry type: '{s}'. Use 'internet' or 'device'"
            )),
        }
    }
}

/// Telemetry analysis commands
#[derive(Subcommand, Debug)]
pub enum TelemetryCommands {
    #[command(
        about = "Calculate and display telemetry statistics",
        after_help = r#"Examples:
    # View internet telemetry stats for epoch 9
    telemetry stats --type internet --epoch 9

    # View device telemetry stats as CSV
    telemetry stats --type device --epoch 9 --output-format csv --output-file device-stats.csv

    # Filter internet stats by city pair
    telemetry stats --type internet --epoch 9 --from-city nyc --to-city fra

    # Filter device stats by location
    telemetry stats --type device --epoch 9 --city "San Francisco""#
    )]
    Stats {
        /// Telemetry type to analyze (internet or device)
        #[arg(
            short = 't',
            long,
            value_name = "TYPE",
            help = "Telemetry type: 'internet' or 'device'"
        )]
        telemetry_type: TelemetryType,

        /// DZ epoch to analyze
        #[arg(short, long, value_name = "EPOCH")]
        epoch: Option<u64>,

        /// Common filter options
        #[command(flatten)]
        filters: FilterOptions,

        /// Output options
        #[command(flatten)]
        output: OutputOptions,
    },

    #[command(
        about = "Export raw telemetry samples",
        after_help = r#"Examples:
    # Export all internet samples for epoch 9
    telemetry export --type internet --epoch 9 --output-format json --output-file samples.json

    # Export device samples between specific locations
    telemetry export --type device --epoch 9 --city "New York" --output-format csv"#
    )]
    Export {
        /// Telemetry type to export (internet or device)
        #[arg(
            short = 't',
            long,
            value_name = "TYPE",
            help = "Telemetry type: 'internet' or 'device'"
        )]
        telemetry_type: TelemetryType,

        /// DZ epoch to export
        #[arg(short, long, value_name = "EPOCH")]
        epoch: Option<u64>,

        /// Common filter options
        #[command(flatten)]
        filters: FilterOptions,

        /// Output options
        #[command(flatten)]
        output: OutputOptions,
    },

    #[command(
        about = "Analyze telemetry quality and identify problematic connections",
        after_help = r#"Examples:
    # Find high latency internet links
    telemetry analyze --type internet --epoch 9 --threshold-ms 200

    # Find device links with packet loss
    telemetry analyze --type device --epoch 9 --min-packet-loss 0.01

    # Export analysis results
    telemetry analyze --type internet --epoch 9 --output-format csv --output-file issues.csv"#
    )]
    Analyze {
        /// Telemetry type to analyze (internet or device)
        #[arg(
            short = 't',
            long,
            value_name = "TYPE",
            help = "Telemetry type: 'internet' or 'device'"
        )]
        telemetry_type: TelemetryType,

        /// DZ epoch to analyze
        #[arg(short, long, value_name = "EPOCH")]
        epoch: Option<u64>,

        /// Analysis thresholds
        #[command(flatten)]
        thresholds: ThresholdOptions,

        /// Output options
        #[command(flatten)]
        output: OutputOptions,
    },

    #[command(
        about = "Calculate rent requirements for telemetry sample accounts",
        after_help = r#"Examples:
    # Calculate rent for device telemetry for the last epoch
    telemetry rent --type device

    # Calculate rent for internet telemetry for a specific epoch
    telemetry rent --type internet --epoch 123

    # Calculate rent for both types
    telemetry rent --type all

    # Export rent analysis as JSON
    telemetry rent --type all --epoch 123 --output-format json --output-file rent-analysis.json"#
    )]
    Rent {
        /// Telemetry type to analyze (device, internet, or all)
        #[arg(
            short = 't',
            long,
            value_name = "TYPE",
            default_value = "all",
            help = "Telemetry type: 'device', 'internet', or 'all'"
        )]
        telemetry_type: TelemetryType,

        /// DZ epoch to analyze (defaults to last epoch)
        #[arg(short, long, value_name = "EPOCH")]
        epoch: Option<u64>,

        /// Output options
        #[command(flatten)]
        output: OutputOptions,
    },
}

/// Internet telemetry statistics export
#[derive(Debug, Serialize, Deserialize)]
pub struct InternetStatsExport {
    pub epoch: u64,
    pub total_links: usize,
    pub total_samples: usize,
    pub stats: Vec<InternetLinkStats>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct InternetLinkStats {
    pub from_city: String,
    pub to_city: String,
    pub samples: usize,
    pub mean_latency_ms: f64,
    pub median_latency_ms: f64,
    pub p95_latency_ms: f64,
    pub p99_latency_ms: f64,
    pub packet_loss: f64,
    pub jitter_ms: f64,
}

/// Device telemetry statistics export
#[derive(Debug, Serialize, Deserialize)]
pub struct DeviceStatsExport {
    pub epoch: u64,
    pub total_circuits: usize,
    pub total_samples: usize,
    pub stats: Vec<DeviceLinkStats>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct DeviceLinkStats {
    pub circuit: String,
    pub city: String,
    pub exchange: String,
    pub samples: usize,
    pub mean_latency_ms: f64,
    pub median_latency_ms: f64,
    pub p95_latency_ms: f64,
    pub p99_latency_ms: f64,
    pub packet_loss: f64,
    pub jitter_ms: f64,
    pub uptime: f64,
    pub bandwidth_mbps: f64,
}

/// Link quality analysis results
#[derive(Debug, Serialize, Deserialize)]
pub struct LinkQualityAnalysis {
    pub epoch: u64,
    pub telemetry_type: String,
    pub issues_found: usize,
    pub problematic_links: Vec<ProblematicLink>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct ProblematicLink {
    pub from_location: String,
    pub to_location: String,
    pub issue_type: String,
    pub severity: String,
    pub mean_latency_ms: f64,
    pub packet_loss: f64,
    pub jitter_ms: f64,
    pub samples: usize,
}

/// Rent analysis for telemetry sample accounts
#[derive(Debug, Serialize, Deserialize)]
pub struct TelemetryRentAnalysis {
    pub epoch: u64,
    pub device_telemetry: Option<TelemetryTypeRentAnalysis>,
    pub internet_telemetry: Option<TelemetryTypeRentAnalysis>,
    pub combined_summary: RentSummary,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct TelemetryTypeRentAnalysis {
    pub telemetry_type: String,
    pub total_accounts: usize,
    pub total_bytes: usize,
    pub lamports_per_byte: u64,
    pub total_rent_lamports: u64,
    pub total_rent_sol: f64,
    pub average_account_size_bytes: usize,
    pub account_details: Vec<AccountRentDetail>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct RentSummary {
    pub total_accounts: usize,
    pub total_bytes: usize,
    pub total_rent_lamports: u64,
    pub total_rent_sol: f64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AccountRentDetail {
    pub pubkey: String,
    pub size_bytes: usize,
    pub rent_lamports: u64,
}

// Tabled structs for displaying rent information
#[derive(Tabled)]
struct RentSummaryRow {
    #[tabled(rename = "Telemetry Type")]
    telemetry_type: String,
    #[tabled(rename = "Accounts")]
    accounts: String,
    #[tabled(rename = "Total Size")]
    total_size: String,
    #[tabled(rename = "Avg Size")]
    avg_size: String,
    #[tabled(rename = "Rent (SOL)")]
    rent_sol: String,
}

#[derive(Tabled)]
struct RentTotalRow {
    #[tabled(rename = "Category")]
    category: String,
    #[tabled(rename = "Value")]
    value: String,
}

// Implement Exportable traits
impl Exportable for InternetStatsExport {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => collection_to_csv(&self.stats),
            OutputFormat::Json => to_json_string(self, false),
            OutputFormat::JsonPretty => to_json_string(self, true),
        }
    }
}

impl Exportable for DeviceStatsExport {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => collection_to_csv(&self.stats),
            OutputFormat::Json => to_json_string(self, false),
            OutputFormat::JsonPretty => to_json_string(self, true),
        }
    }
}

impl Exportable for LinkQualityAnalysis {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => collection_to_csv(&self.problematic_links),
            OutputFormat::Json => to_json_string(self, false),
            OutputFormat::JsonPretty => to_json_string(self, true),
        }
    }
}

impl Exportable for TelemetryRentAnalysis {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => {
                // For CSV, combine all account details from both types
                let mut all_details = Vec::new();
                if let Some(ref device) = self.device_telemetry {
                    all_details.extend(device.account_details.clone());
                }
                if let Some(ref internet) = self.internet_telemetry {
                    all_details.extend(internet.account_details.clone());
                }
                collection_to_csv(&all_details)
            }
            OutputFormat::Json => to_json_string(self, false),
            OutputFormat::JsonPretty => to_json_string(self, true),
        }
    }
}

/// Handle telemetry commands
pub async fn handle(orchestrator: &Orchestrator, cmd: TelemetryCommands) -> Result<()> {
    match cmd {
        TelemetryCommands::Stats {
            telemetry_type,
            epoch,
            filters,
            output,
        } => match telemetry_type {
            TelemetryType::Internet => {
                handle_internet_stats(orchestrator, epoch, filters, output).await
            }
            TelemetryType::Device => {
                handle_device_stats(orchestrator, epoch, filters, output).await
            }
            TelemetryType::All => {
                handle_internet_stats(orchestrator, epoch, filters.clone(), output.clone()).await?;
                handle_device_stats(orchestrator, epoch, filters, output).await
            }
        },
        TelemetryCommands::Export {
            telemetry_type,
            epoch,
            filters,
            output,
        } => match telemetry_type {
            TelemetryType::Internet => {
                handle_internet_export(orchestrator, epoch, filters, output).await
            }
            TelemetryType::Device => {
                handle_device_export(orchestrator, epoch, filters, output).await
            }
            TelemetryType::All => {
                handle_internet_export(orchestrator, epoch, filters.clone(), output.clone())
                    .await?;
                handle_device_export(orchestrator, epoch, filters, output).await
            }
        },
        TelemetryCommands::Analyze {
            telemetry_type,
            epoch,
            thresholds,
            output,
        } => match telemetry_type {
            TelemetryType::Internet => {
                handle_internet_analyze(orchestrator, epoch, thresholds, output).await
            }
            TelemetryType::Device => {
                handle_device_analyze(orchestrator, epoch, thresholds, output).await
            }
            TelemetryType::All => {
                handle_internet_analyze(orchestrator, epoch, thresholds.clone(), output.clone())
                    .await?;
                handle_device_analyze(orchestrator, epoch, thresholds, output).await
            }
        },
        TelemetryCommands::Rent {
            telemetry_type,
            epoch,
            output,
        } => handle_telemetry_rent_analysis(orchestrator, telemetry_type, epoch, output).await,
    }
}

async fn handle_internet_stats(
    orchestrator: &Orchestrator,
    epoch: Option<u64>,
    filters: FilterOptions,
    output: OutputOptions,
) -> Result<()> {
    info!("Calculating internet telemetry statistics");

    // Create fetcher
    let fetcher = Fetcher::from_settings(orchestrator.settings())?;

    // Fetch data for epoch
    let (fetch_epoch, fetch_data) = fetcher.fetch(epoch).await?;

    info!("Processing telemetry for epoch {}", fetch_epoch);

    // Process internet telemetry
    let internet_stats = InternetTelemetryProcessor::process(&fetch_data)?;

    // Filter stats if requested
    let filtered_stats: BTreeMap<String, InternetTelemetryStats> = internet_stats
        .into_iter()
        .filter(|(key, _)| {
            let parts: Vec<&str> = key.split('_').collect();
            if parts.len() != 2 {
                return false;
            }
            let origin = parts[0];
            let target = parts[1];

            let from_match = filters
                .from_city
                .as_ref()
                .is_none_or(|city| origin.contains(city));
            let to_match = filters
                .to_city
                .as_ref()
                .is_none_or(|city| target.contains(city));

            from_match && to_match
        })
        .collect();

    // Convert to export format
    let mut stats_list = Vec::new();
    for (route, stats) in &filtered_stats {
        let parts: Vec<&str> = route.split('_').collect();
        if parts.len() == 2 {
            // Extract city codes from exchange codes (remove 'x' prefix)
            let from_city = parts[0].trim_start_matches('x').to_string();
            let to_city = parts[1].trim_start_matches('x').to_string();

            stats_list.push(InternetLinkStats {
                from_city,
                to_city,
                samples: stats.total_samples,
                mean_latency_ms: stats.rtt_mean_us / 1000.0,
                median_latency_ms: stats.rtt_median_us / 1000.0,
                p95_latency_ms: stats.rtt_p95_us / 1000.0,
                p99_latency_ms: stats.rtt_p99_us / 1000.0,
                packet_loss: stats.packet_loss,
                jitter_ms: stats.avg_jitter_us / 1000.0,
            });
        }
    }

    let stats_export = InternetStatsExport {
        epoch: fetch_epoch,
        total_links: stats_list.len(),
        total_samples: fetch_data.dz_internet.internet_latency_samples.len(),
        stats: stats_list,
    };

    // Export based on options
    let export_options = OutputOptions {
        output_format: output.output_format,
        output_dir: output.output_dir.clone(),
        output_file: output.output_file.clone(),
    };

    let default_filename = format!("internet-stats-epoch-{fetch_epoch}");
    export_options.write(&stats_export, &default_filename)?;

    info!("Internet telemetry statistics exported successfully");
    Ok(())
}

async fn handle_device_stats(
    orchestrator: &Orchestrator,
    epoch: Option<u64>,
    filters: FilterOptions,
    output: OutputOptions,
) -> Result<()> {
    info!("Calculating device telemetry statistics");

    // Create fetcher
    let fetcher = Fetcher::from_settings(orchestrator.settings())?;

    // Fetch data for epoch
    let (fetch_epoch, fetch_data) = fetcher.fetch(epoch).await?;

    info!("Processing telemetry for epoch {}", fetch_epoch);

    // Process device telemetry
    let device_stats = DZDTelemetryProcessor::process(&fetch_data)?;

    // Get city for filtering (prefer city over from_city)
    let city_filter = filters.city.or(filters.from_city);

    // Filter stats if requested
    let filtered_stats: BTreeMap<String, DZDTelemetryStats> = device_stats
        .into_iter()
        .filter(|(_, stats)| {
            // Filter by city if specified
            if let Some(ref city) = city_filter
                && let Some(location) = fetch_data.get_device_location(&stats.origin_device)
                && !location.name.to_lowercase().contains(&city.to_lowercase())
            {
                return false;
            }

            // Filter by device ID if specified
            if let Some(ref device_id) = filters.device
                && stats.origin_device.to_string() != *device_id
                && stats.target_device.to_string() != *device_id
            {
                return false;
            }

            // Note: exchange filter removed from common FilterOptions
            // To re-enable, add exchange field to FilterOptions

            true
        })
        .collect();

    // Convert to export format
    let mut stats_list = Vec::new();
    for stats in filtered_stats.values() {
        // Get location for origin device
        let location = fetch_data
            .get_device_location(&stats.origin_device)
            .map(|l| l.name.clone())
            .unwrap_or_else(|| "Unknown".to_string());

        // Get exchange for origin device
        let device = fetch_data
            .dz_serviceability
            .devices
            .get(&stats.origin_device);
        let exchange = device
            .and_then(|d| fetch_data.dz_serviceability.exchanges.get(&d.exchange_pk))
            .map(|e| e.code.clone())
            .unwrap_or_else(|| "Unknown".to_string());

        stats_list.push(DeviceLinkStats {
            circuit: stats.circuit.clone(),
            city: location,
            exchange,
            samples: stats.total_samples,
            mean_latency_ms: stats.rtt_mean_us / 1000.0,
            median_latency_ms: stats.rtt_median_us / 1000.0,
            p95_latency_ms: stats.rtt_p95_us / 1000.0,
            p99_latency_ms: stats.rtt_p99_us / 1000.0,
            packet_loss: stats.packet_loss,
            jitter_ms: stats.avg_jitter_us / 1000.0,
            uptime: 1.0,            // Default for now
            bandwidth_mbps: 1000.0, // Default for now
        });
    }

    let stats_export = DeviceStatsExport {
        epoch: fetch_epoch,
        total_circuits: stats_list.len(),
        total_samples: fetch_data.dz_telemetry.device_latency_samples.len(),
        stats: stats_list,
    };

    // Export based on options
    let export_options = OutputOptions {
        output_format: output.output_format,
        output_dir: output.output_dir.clone(),
        output_file: output.output_file.clone(),
    };

    let default_filename = format!("device-stats-epoch-{fetch_epoch}");
    export_options.write(&stats_export, &default_filename)?;

    info!("Device telemetry statistics exported successfully");
    Ok(())
}

async fn handle_internet_export(
    orchestrator: &Orchestrator,
    epoch: Option<u64>,
    filters: FilterOptions,
    output: OutputOptions,
) -> Result<()> {
    info!("Exporting internet telemetry samples");

    // Create fetcher
    let fetcher = Fetcher::from_settings(orchestrator.settings())?;

    // Fetch data for epoch
    let (fetch_epoch, fetch_data) = fetcher.fetch(epoch).await?;

    // Filter samples if requested
    let samples = if filters.from_city.is_some() || filters.to_city.is_some() {
        let exchanges = &fetch_data.dz_serviceability.exchanges;
        fetch_data
            .dz_internet
            .internet_latency_samples
            .into_iter()
            .filter(|sample| {
                let origin_exchange = exchanges.get(&sample.origin_exchange_pk);
                let target_exchange = exchanges.get(&sample.target_exchange_pk);

                if let (Some(origin), Some(target)) = (origin_exchange, target_exchange) {
                    let from_match = filters
                        .from_city
                        .as_ref()
                        .is_none_or(|city| origin.code.contains(city));
                    let to_match = filters
                        .to_city
                        .as_ref()
                        .is_none_or(|city| target.code.contains(city));
                    from_match && to_match
                } else {
                    false
                }
            })
            .collect()
    } else {
        fetch_data.dz_internet.internet_latency_samples
    };

    info!("Exporting {} samples", samples.len());

    // Export based on options
    let export_options = OutputOptions {
        output_format: output.output_format,
        output_dir: output.output_dir.clone(),
        output_file: output.output_file.clone(),
    };

    // Create export wrapper
    #[derive(Serialize)]
    struct SamplesExport {
        epoch: u64,
        count: usize,
        samples: Vec<DZInternetLatencySamples>,
    }

    let export_data = SamplesExport {
        epoch: fetch_epoch,
        count: samples.len(),
        samples,
    };

    let default_filename = format!("internet-samples-epoch-{fetch_epoch}");

    // Manual export since we don't have Exportable for raw samples
    let output_str = match output.output_format {
        OutputFormat::Csv => {
            bail!("Unsupported! Use --output-format json or json-pretty instead.")
        }
        OutputFormat::Json => to_json_string(&export_data, false)?,
        OutputFormat::JsonPretty => to_json_string(&export_data, true)?,
    };

    if let Some(ref file) = export_options.output_file {
        std::fs::write(file, output_str)?;
        info!("Exported to {}", file);
    } else if let Some(ref dir) = export_options.output_dir {
        std::fs::create_dir_all(dir)?;
        let ext = match export_options.output_format {
            OutputFormat::Csv => "csv",
            _ => "json",
        };
        let path = format!("{dir}/{default_filename}.{ext}");
        std::fs::write(&path, output_str)?;
        info!("Exported to {path}");
    } else {
        println!("{output_str}");
    }

    Ok(())
}

async fn handle_device_export(
    orchestrator: &Orchestrator,
    epoch: Option<u64>,
    filters: FilterOptions,
    output: OutputOptions,
) -> Result<()> {
    info!("Exporting device telemetry samples");

    // Create fetcher
    let fetcher = Fetcher::from_settings(orchestrator.settings())?;

    // Fetch data for epoch
    let (fetch_epoch, fetch_data) = fetcher.fetch(epoch).await?;

    // Get city for filtering (prefer city over from_city)
    let city_filter = filters.city.or(filters.from_city);

    // Filter samples if requested
    let samples = if city_filter.is_some() || filters.device.is_some() {
        let _exchanges = &fetch_data.dz_serviceability.exchanges;
        fetch_data
            .dz_telemetry
            .device_latency_samples
            .into_iter()
            .filter(|sample| {
                let device_match = filters.device.as_ref().is_none_or(|id| {
                    sample.origin_device_pk.to_string() == *id
                        || sample.target_device_pk.to_string() == *id
                });

                let city_match = city_filter.is_none(); // Skip city filtering for now

                device_match && city_match
            })
            .collect()
    } else {
        fetch_data.dz_telemetry.device_latency_samples
    };

    info!("Exporting {} samples", samples.len());

    // Export based on options
    let export_options = OutputOptions {
        output_format: output.output_format,
        output_dir: output.output_dir.clone(),
        output_file: output.output_file.clone(),
    };

    // Create export wrapper
    #[derive(Serialize)]
    struct SamplesExport {
        epoch: u64,
        count: usize,
        samples: Vec<DZDeviceLatencySamples>,
    }

    let export_data = SamplesExport {
        epoch: fetch_epoch,
        count: samples.len(),
        samples,
    };

    let default_filename = format!("device-samples-epoch-{fetch_epoch}");

    // Manual export since we don't have Exportable for raw samples
    let output_str = match output.output_format {
        OutputFormat::Csv => {
            bail!("Unsupported! Use --output-format json or json-pretty instead.")
        }
        OutputFormat::Json => to_json_string(&export_data, false)?,
        OutputFormat::JsonPretty => to_json_string(&export_data, true)?,
    };

    if let Some(ref file) = export_options.output_file {
        std::fs::write(file, output_str)?;
        info!("Exported to {}", file);
    } else if let Some(ref dir) = export_options.output_dir {
        std::fs::create_dir_all(dir)?;
        let ext = match export_options.output_format {
            OutputFormat::Csv => "csv",
            _ => "json",
        };
        let path = format!("{dir}/{default_filename}.{ext}");
        std::fs::write(&path, output_str)?;
        info!("Exported to {path}");
    } else {
        println!("{output_str}");
    }

    Ok(())
}

async fn handle_internet_analyze(
    orchestrator: &Orchestrator,
    epoch: Option<u64>,
    thresholds: ThresholdOptions,
    output: OutputOptions,
) -> Result<()> {
    info!("Analyzing internet link quality");

    // Create fetcher
    let fetcher = Fetcher::from_settings(orchestrator.settings())?;

    // Fetch data for epoch
    let (fetch_epoch, fetch_data) = fetcher.fetch(epoch).await?;

    // Process internet telemetry
    let internet_stats = InternetTelemetryProcessor::process(&fetch_data)?;

    // Default thresholds
    let latency_threshold = thresholds.threshold_ms.unwrap_or(200.0);
    let packet_loss_threshold = thresholds.min_packet_loss.unwrap_or(0.01);
    let jitter_threshold = thresholds.min_jitter.unwrap_or(50.0);

    // Find problematic links
    let mut problematic_links = Vec::new();
    for (route, stats) in &internet_stats {
        let parts: Vec<&str> = route.split('_').collect();
        if parts.len() != 2 {
            continue;
        }

        let from_city = parts[0].trim_start_matches('x').to_string();
        let to_city = parts[1].trim_start_matches('x').to_string();

        let mut issues = Vec::new();
        let mut severity = "low";

        let mean_latency_ms = stats.rtt_mean_us / 1000.0;
        let jitter_ms = stats.avg_jitter_us / 1000.0;

        if mean_latency_ms > latency_threshold {
            issues.push("high_latency");
            if mean_latency_ms > latency_threshold * 2.0 {
                severity = "high";
            } else {
                severity = "medium";
            }
        }

        if stats.packet_loss > packet_loss_threshold {
            issues.push("packet_loss");
            if stats.packet_loss > 0.05 {
                severity = "high";
            } else if severity == "low" {
                severity = "medium";
            }
        }

        if jitter_ms > jitter_threshold {
            issues.push("high_jitter");
            if severity == "low" {
                severity = "medium";
            }
        }

        if !issues.is_empty() {
            problematic_links.push(ProblematicLink {
                from_location: from_city,
                to_location: to_city,
                issue_type: issues.join(", "),
                severity: severity.to_string(),
                mean_latency_ms,
                packet_loss: stats.packet_loss,
                jitter_ms,
                samples: stats.total_samples,
            });
        }
    }

    // Sort by severity and latency
    problematic_links.sort_by(|a, b| {
        let sev_order = |s: &str| match s {
            "high" => 0,
            "medium" => 1,
            _ => 2,
        };
        sev_order(&a.severity)
            .cmp(&sev_order(&b.severity))
            .then(b.mean_latency_ms.partial_cmp(&a.mean_latency_ms).unwrap())
    });

    let analysis = LinkQualityAnalysis {
        epoch: fetch_epoch,
        telemetry_type: "internet".to_string(),
        issues_found: problematic_links.len(),
        problematic_links,
    };

    // Export based on options
    let export_options = OutputOptions {
        output_format: output.output_format,
        output_dir: output.output_dir.clone(),
        output_file: output.output_file.clone(),
    };

    let default_filename = format!("internet-analysis-epoch-{fetch_epoch}");
    export_options.write(&analysis, &default_filename)?;

    info!(
        "Internet link quality analysis complete: {} issues found",
        analysis.issues_found
    );
    Ok(())
}

async fn handle_device_analyze(
    orchestrator: &Orchestrator,
    epoch: Option<u64>,
    thresholds: ThresholdOptions,
    output: OutputOptions,
) -> Result<()> {
    info!("Analyzing device performance");

    // Create fetcher
    let fetcher = Fetcher::from_settings(orchestrator.settings())?;

    // Fetch data for epoch
    let (fetch_epoch, fetch_data) = fetcher.fetch(epoch).await?;

    // Process device telemetry
    let device_stats = DZDTelemetryProcessor::process(&fetch_data)?;

    // Default thresholds
    let latency_threshold = thresholds.threshold_ms.unwrap_or(100.0);
    let _uptime_threshold = thresholds.min_uptime.unwrap_or(0.95);
    // Note: min_bandwidth removed from common ThresholdOptions
    // To re-enable, add min_bandwidth field to ThresholdOptions

    // Find problematic devices
    let mut problematic_links = Vec::new();
    for (route, stats) in &device_stats {
        let parts: Vec<&str> = route.split('_').collect();
        if parts.len() != 2 {
            continue;
        }

        let from_device = parts[0].to_string();
        let to_device = parts[1].to_string();

        let mut issues = Vec::new();
        let mut severity = "low";

        let mean_latency_ms = stats.rtt_mean_us / 1000.0;
        let jitter_ms = stats.avg_jitter_us / 1000.0;

        if mean_latency_ms > latency_threshold {
            issues.push("high_latency");
            if mean_latency_ms > latency_threshold * 2.0 {
                severity = "high";
            } else {
                severity = "medium";
            }
        }

        if stats.packet_loss > 0.01 {
            issues.push("packet_loss");
            if stats.packet_loss > 0.05 {
                severity = "high";
            } else if severity == "low" {
                severity = "medium";
            }
        }

        if jitter_ms > 20.0 {
            issues.push("high_jitter");
            if severity == "low" {
                severity = "medium";
            }
        }

        if !issues.is_empty() {
            problematic_links.push(ProblematicLink {
                from_location: from_device,
                to_location: to_device,
                issue_type: issues.join(", "),
                severity: severity.to_string(),
                mean_latency_ms,
                packet_loss: stats.packet_loss,
                jitter_ms,
                samples: stats.total_samples,
            });
        }
    }

    // Sort by severity and latency
    problematic_links.sort_by(|a, b| {
        let sev_order = |s: &str| match s {
            "high" => 0,
            "medium" => 1,
            _ => 2,
        };
        sev_order(&a.severity)
            .cmp(&sev_order(&b.severity))
            .then(b.mean_latency_ms.partial_cmp(&a.mean_latency_ms).unwrap())
    });

    let analysis = LinkQualityAnalysis {
        epoch: fetch_epoch,
        telemetry_type: "device".to_string(),
        issues_found: problematic_links.len(),
        problematic_links,
    };

    // Export based on options
    let export_options = OutputOptions {
        output_format: output.output_format,
        output_dir: output.output_dir.clone(),
        output_file: output.output_file.clone(),
    };

    let default_filename = format!("device-analysis-epoch-{fetch_epoch}");
    export_options.write(&analysis, &default_filename)?;

    info!(
        "Device performance analysis complete: {} issues found",
        analysis.issues_found
    );
    Ok(())
}

async fn handle_telemetry_rent_analysis(
    orchestrator: &Orchestrator,
    telemetry_type: TelemetryType,
    epoch: Option<u64>,
    output: OutputOptions,
) -> Result<()> {
    const LAMPORTS_PER_BYTE: u64 = 6_960;
    const LAMPORTS_PER_SOL: f64 = 1_000_000_000.0;

    info!("Calculating rent requirements for telemetry accounts");

    // Fetch all data
    let fetcher = Fetcher::from_settings(orchestrator.settings())?;
    let (fetch_epoch, fetch_data) = fetcher.fetch(epoch).await?;

    // Helper function to analyze account rent
    fn analyze_rent(accounts: &KeyedAccounts, telemetry_type: &str) -> TelemetryTypeRentAnalysis {
        let mut total_bytes = 0usize;
        let mut account_details = Vec::new();

        for (pubkey, account) in accounts {
            let size = account.data.len();
            total_bytes += size;

            account_details.push(AccountRentDetail {
                pubkey: pubkey.to_string(),
                size_bytes: size,
                rent_lamports: size as u64 * LAMPORTS_PER_BYTE,
            });
        }

        let total_rent_lamports = total_bytes as u64 * LAMPORTS_PER_BYTE;
        let total_rent_sol = total_rent_lamports as f64 / LAMPORTS_PER_SOL;
        let avg_size = if accounts.is_empty() {
            0
        } else {
            total_bytes / accounts.len()
        };

        TelemetryTypeRentAnalysis {
            telemetry_type: telemetry_type.to_string(),
            total_accounts: accounts.len(),
            total_bytes,
            lamports_per_byte: LAMPORTS_PER_BYTE,
            total_rent_lamports,
            total_rent_sol,
            average_account_size_bytes: avg_size,
            account_details,
        }
    }

    // Analyze accounts based on type using the fetched data
    let mut device_analysis = None;
    let mut internet_analysis = None;

    match telemetry_type {
        TelemetryType::Device => {
            device_analysis = Some(analyze_rent(&fetch_data.dz_telemetry.accounts, "device"));
        }
        TelemetryType::Internet => {
            internet_analysis = Some(analyze_rent(&fetch_data.dz_internet.accounts, "internet"));
        }
        TelemetryType::All => {
            device_analysis = Some(analyze_rent(&fetch_data.dz_telemetry.accounts, "device"));
            internet_analysis = Some(analyze_rent(&fetch_data.dz_internet.accounts, "internet"));
        }
    }

    // Calculate combined summary
    let mut total_accounts = 0;
    let mut total_bytes = 0;
    let mut total_rent_lamports = 0;

    if let Some(ref device) = device_analysis {
        total_accounts += device.total_accounts;
        total_bytes += device.total_bytes;
        total_rent_lamports += device.total_rent_lamports;
    }

    if let Some(ref internet) = internet_analysis {
        total_accounts += internet.total_accounts;
        total_bytes += internet.total_bytes;
        total_rent_lamports += internet.total_rent_lamports;
    }

    let combined_summary = RentSummary {
        total_accounts,
        total_bytes,
        total_rent_lamports,
        total_rent_sol: total_rent_lamports as f64 / LAMPORTS_PER_SOL,
    };

    let analysis = TelemetryRentAnalysis {
        epoch: fetch_epoch,
        device_telemetry: device_analysis,
        internet_telemetry: internet_analysis,
        combined_summary,
    };

    // Display summary
    println!();
    println!("Telemetry Accounts Rent Analysis - Epoch {fetch_epoch}");
    println!("=================================================\n");

    // Build table rows
    let mut rows = Vec::new();

    if let Some(ref device) = analysis.device_telemetry {
        rows.push(RentSummaryRow {
            telemetry_type: "Device".to_string(),
            accounts: format!("{}", device.total_accounts),
            total_size: format!(
                "{} bytes ({:.2} MB)",
                device.total_bytes,
                device.total_bytes as f64 / (1024.0 * 1024.0)
            ),
            avg_size: format!("{} bytes", device.average_account_size_bytes),
            rent_sol: format!("{:.6} SOL", device.total_rent_sol),
        });
    }

    if let Some(ref internet) = analysis.internet_telemetry {
        rows.push(RentSummaryRow {
            telemetry_type: "Internet".to_string(),
            accounts: format!("{}", internet.total_accounts),
            total_size: format!(
                "{} bytes ({:.2} MB)",
                internet.total_bytes,
                internet.total_bytes as f64 / (1024.0 * 1024.0)
            ),
            avg_size: format!("{} bytes", internet.average_account_size_bytes),
            rent_sol: format!("{:.6} SOL", internet.total_rent_sol),
        });
    }

    // Display telemetry breakdown table
    if !rows.is_empty() {
        let table = Table::new(rows).with(Style::psql()).to_string();
        println!("{table}");
    }

    // Display combined summary table
    println!("\nCombined Summary:");
    let total_rows = vec![
        RentTotalRow {
            category: "Total Accounts".to_string(),
            value: format!("{}", analysis.combined_summary.total_accounts),
        },
        RentTotalRow {
            category: "Total Size".to_string(),
            value: format!(
                "{} bytes ({:.2} MB)",
                analysis.combined_summary.total_bytes,
                analysis.combined_summary.total_bytes as f64 / (1024.0 * 1024.0)
            ),
        },
        RentTotalRow {
            category: "Rent per Byte".to_string(),
            value: format!("{LAMPORTS_PER_BYTE} lamports"),
        },
        RentTotalRow {
            category: "Total Rent Required".to_string(),
            value: format!(
                "{:.6} SOL ({} lamports)",
                analysis.combined_summary.total_rent_sol,
                analysis.combined_summary.total_rent_lamports
            ),
        },
    ];

    let total_table = Table::new(total_rows).with(Style::psql()).to_string();
    println!("{total_table}");
    println!();

    // Export based on options
    let export_options = OutputOptions {
        output_format: output.output_format,
        output_dir: output.output_dir.clone(),
        output_file: output.output_file.clone(),
    };

    let default_filename = format!("telemetry-rent-analysis-epoch-{fetch_epoch}");
    export_options.write(&analysis, &default_filename)?;

    info!("Telemetry accounts rent analysis complete");
    Ok(())
}
