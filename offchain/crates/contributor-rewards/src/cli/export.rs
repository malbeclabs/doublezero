//! Export commands for exporting Shapley calculation data.

use std::{
    collections::BTreeMap,
    fs::{File, create_dir_all},
    io::Write,
    path::{Path, PathBuf},
};

use anyhow::{Result, bail};
use clap::Subcommand;
use network_shapley::types::{Demand, Demands, Devices, PrivateLinks, PublicLinks};
use tracing::info;

use crate::{
    calculator::{
        data_prep::PreparedData, orchestrator::Orchestrator,
        shapley::evaluator::compute_shapley_values,
    },
    cli::{
        common::{OutputFormat, OutputOptions, collection_to_csv, to_json_string},
        snapshot::CompleteSnapshot,
        traits::Exportable,
    },
    ingestor::demand::CityStats,
    settings::ShapleySettings,
};

/// Export commands for data extraction
#[derive(Subcommand, Debug)]
pub enum ExportCommands {
    #[command(
        about = "Run full Shapley calculation and export inputs, per-city values, and aggregated output",
        after_help = r#"Examples:
    # Export to stdout as pretty JSON (default)
    export shapley -s snapshot.json

    # Export to specific file as compact JSON
    export shapley -s snapshot.json -f json --output-file debug.json

    # Export to directory as CSV (creates separate files for inputs and outputs)
    export shapley -s snapshot.json -f csv -o ./debug-output/

    # Export to directory as CSV with demands split by origin city
    export shapley -s snapshot.json -f csv -o ./debug-output/ -c

    # Export to directory as JSON (creates single shapley-epoch-N.json)
    export shapley -s snapshot.json -f json-pretty -o ./debug-output/"#
    )]
    Shapley {
        /// Path to snapshot file (required)
        #[arg(short = 's', long, value_name = "FILE", required = true)]
        snapshot: PathBuf,

        /// Output format for exports
        #[arg(short = 'f', long, default_value = "json-pretty")]
        output_format: OutputFormat,

        /// Directory to export files (required for CSV format)
        #[arg(short = 'o', long, value_name = "DIR")]
        output_dir: Option<PathBuf>,

        /// Specific output file path (JSON formats only)
        #[arg(long, value_name = "FILE")]
        output_file: Option<PathBuf>,

        /// Split demands CSV by origin city (creates separate files per city)
        #[arg(short = 'c', long)]
        split_by_city: bool,
    },
}

// ============================================================================
// Shapley Export Output Structures
// ============================================================================

/// Complete Shapley export output containing inputs and computed values
#[derive(Debug, serde::Serialize, serde::Deserialize)]
pub struct ShapleyExportOutput {
    pub epoch: u64,
    pub shapley_settings: ShapleySettings,
    pub inputs: ShapleyExportInputs,
    pub per_city_values: BTreeMap<String, Vec<OperatorValue>>,
    pub aggregated_output: Vec<AggregatedShapleyRow>,
}

/// Input data for Shapley calculation
#[derive(Debug, serde::Serialize, serde::Deserialize)]
pub struct ShapleyExportInputs {
    pub devices: Devices,
    pub private_links: PrivateLinks,
    pub public_links: PublicLinks,
    pub demands: Demands,
    pub city_stats: CityStats,
    pub city_weights: BTreeMap<String, f64>,
}

/// Per-city operator value
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct OperatorValue {
    pub operator: String,
    pub value: f64,
}

/// CSV row for per-city Shapley values
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct PerCityShapleyRow {
    pub city: String,
    pub operator: String,
    pub shapley_value: f64,
}

/// CSV row for aggregated Shapley values
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct AggregatedShapleyRow {
    pub operator: String,
    pub value: f64,
    pub proportion: f64,
}

impl Exportable for ShapleyExportOutput {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => {
                bail!(
                    "CSV export requires --output-dir to create separate files for inputs and outputs"
                )
            }
            OutputFormat::Json => to_json_string(self, false),
            OutputFormat::JsonPretty => to_json_string(self, true),
        }
    }
}

/// Handle export commands
pub async fn handle(orchestrator: &Orchestrator, cmd: ExportCommands) -> Result<()> {
    match cmd {
        ExportCommands::Shapley {
            snapshot,
            output_format,
            output_dir,
            output_file,
            split_by_city,
        } => {
            handle_export_shapley(
                orchestrator,
                snapshot,
                output_format,
                output_dir,
                output_file,
                split_by_city,
            )
            .await
        }
    }
}

// ============================================================================
// Shapley Export Handler
// ============================================================================

async fn handle_export_shapley(
    orchestrator: &Orchestrator,
    snapshot_path: PathBuf,
    output_format: OutputFormat,
    output_dir: Option<PathBuf>,
    output_file: Option<PathBuf>,
    split_by_city: bool,
) -> Result<()> {
    // Validate CSV requires output_dir
    if matches!(output_format, OutputFormat::Csv) && output_dir.is_none() {
        bail!("CSV format requires --output-dir to create separate files");
    }

    info!("Loading snapshot from: {:?}", snapshot_path);
    let snapshot = CompleteSnapshot::load_from_file(&snapshot_path)?;

    // Validate snapshot has leader schedule
    if snapshot.leader_schedule.is_none() {
        bail!(
            "Snapshot {:?} missing leader schedule - cannot compute Shapley values",
            snapshot_path
        );
    }

    info!(
        "Snapshot loaded: epoch {}, created at {}",
        snapshot.dz_epoch, snapshot.metadata.created_at
    );

    // Prepare data using shared function
    let prep_data = PreparedData::from_snapshot(&snapshot, orchestrator.settings(), true)?;
    let epoch = prep_data.epoch;

    let shapley_inputs = prep_data
        .shapley_inputs
        .ok_or_else(|| anyhow::anyhow!("Failed to prepare Shapley inputs from snapshot"))?;

    info!(
        "Prepared Shapley inputs: {} devices, {} private links, {} public links, {} demands",
        shapley_inputs.devices.len(),
        shapley_inputs.private_links.len(),
        shapley_inputs.public_links.len(),
        shapley_inputs.demands.len()
    );

    // Compute Shapley values
    info!("Computing Shapley values...");
    let compute_result = compute_shapley_values(&shapley_inputs, &orchestrator.settings().shapley)?;

    // Build per-city values map
    let per_city_values: BTreeMap<String, Vec<OperatorValue>> = compute_result
        .per_city_outputs
        .iter()
        .map(|(city, values)| {
            let operator_values: Vec<OperatorValue> = values
                .iter()
                .map(|(op, val)| OperatorValue {
                    operator: op.clone(),
                    value: *val,
                })
                .collect();
            (city.clone(), operator_values)
        })
        .collect();

    // Build aggregated output
    let aggregated_output: Vec<AggregatedShapleyRow> = compute_result
        .aggregated_output
        .iter()
        .map(|(op, val)| AggregatedShapleyRow {
            operator: op.clone(),
            value: val.value,
            proportion: val.proportion,
        })
        .collect();

    // Build full output
    let export_output = ShapleyExportOutput {
        epoch,
        shapley_settings: orchestrator.settings().shapley.clone(),
        inputs: ShapleyExportInputs {
            devices: shapley_inputs.devices,
            private_links: shapley_inputs.private_links,
            public_links: shapley_inputs.public_links,
            demands: shapley_inputs.demands,
            city_stats: shapley_inputs.city_stats,
            city_weights: shapley_inputs.city_weights,
        },
        per_city_values,
        aggregated_output,
    };

    // Get Solana epoch from snapshot (prefer leader_schedule, fallback to snapshot.solana_epoch)
    let solana_epoch = snapshot
        .leader_schedule
        .as_ref()
        .map(|ls| ls.solana_epoch)
        .or(snapshot.solana_epoch);

    // Handle output
    match output_format {
        OutputFormat::Csv => {
            // CSV mode - write separate files to output_dir
            let dir = output_dir.expect("validated above");
            write_csv_output(&dir, epoch, solana_epoch, &export_output, split_by_city)?;
        }
        OutputFormat::Json | OutputFormat::JsonPretty => {
            let output_options = OutputOptions {
                output_format,
                output_dir: output_dir.map(|p| p.to_string_lossy().to_string()),
                output_file: output_file.map(|p| p.to_string_lossy().to_string()),
            };
            let default_filename = format!("shapley-epoch-{epoch}");
            output_options.write(&export_output, &default_filename)?;
        }
    }

    // Print summary
    println!("\nShapley Export Summary:");
    println!("-----------------------");
    println!("Epoch: {epoch}");
    println!("Cities processed: {}", export_output.per_city_values.len());
    println!("Operators: {}", export_output.aggregated_output.len());
    println!("Devices: {}", export_output.inputs.devices.len());
    println!(
        "Private Links: {}",
        export_output.inputs.private_links.len()
    );
    println!("Public Links: {}", export_output.inputs.public_links.len());
    println!("Demands: {}", export_output.inputs.demands.len());
    println!("\nShapley Settings:");
    println!(
        "  operator_uptime: {}",
        export_output.shapley_settings.operator_uptime
    );
    println!(
        "  contiguity_bonus: {}",
        export_output.shapley_settings.contiguity_bonus
    );
    println!(
        "  demand_multiplier: {}",
        export_output.shapley_settings.demand_multiplier
    );

    Ok(())
}

/// Write CSV output files to directory
fn write_csv_output(
    dir: &Path,
    epoch: u64,
    solana_epoch: Option<u64>,
    output: &ShapleyExportOutput,
    split_by_city: bool,
) -> Result<()> {
    create_dir_all(dir)?;

    // Write devices.csv
    let devices_path = dir.join(format!("devices-epoch-{epoch}.csv"));
    let devices_csv = collection_to_csv(&output.inputs.devices)?;
    File::create(&devices_path)?.write_all(devices_csv.as_bytes())?;
    info!("Exported devices to: {}", devices_path.display());

    // Write private_links.csv
    let private_links_path = dir.join(format!("private-links-epoch-{epoch}.csv"));
    let private_links_csv = collection_to_csv(&output.inputs.private_links)?;
    File::create(&private_links_path)?.write_all(private_links_csv.as_bytes())?;
    info!(
        "Exported private links to: {}",
        private_links_path.display()
    );

    // Write public_links.csv
    let public_links_path = dir.join(format!("public-links-epoch-{epoch}.csv"));
    let public_links_csv = collection_to_csv(&output.inputs.public_links)?;
    File::create(&public_links_path)?.write_all(public_links_csv.as_bytes())?;
    info!("Exported public links to: {}", public_links_path.display());

    // Write demands.csv (either single file or split by origin city)
    if split_by_city {
        // Group demands by origin city (start field)
        let mut demands_by_city: BTreeMap<String, Vec<&Demand>> = BTreeMap::new();
        for demand in &output.inputs.demands {
            demands_by_city
                .entry(demand.start.clone())
                .or_default()
                .push(demand);
        }

        // Write separate CSV file for each origin city
        for (city, city_demands) in demands_by_city {
            let demands_path = dir.join(format!("demand-{city}-epoch-{epoch}.csv"));
            let demands_csv = collection_to_csv(&city_demands)?;
            File::create(&demands_path)?.write_all(demands_csv.as_bytes())?;
            info!(
                "Exported demands for city {} to: {}",
                city,
                demands_path.display()
            );
        }
    } else {
        // Write single demands.csv file
        let demands_path = dir.join(format!("demands-epoch-{epoch}.csv"));
        let demands_csv = collection_to_csv(&output.inputs.demands)?;
        File::create(&demands_path)?.write_all(demands_csv.as_bytes())?;
        info!("Exported demands to: {}", demands_path.display());
    }

    // Write city_stats.csv
    let city_stats_rows: Vec<CityStatRow> = output
        .inputs
        .city_stats
        .iter()
        .map(|(city, stat)| CityStatRow {
            city: city.clone(),
            validator_count: stat.validator_count,
            total_stake_proxy: stat.total_stake_proxy,
        })
        .collect();
    let city_stats_path = dir.join(format!("city-stats-epoch-{epoch}.csv"));
    let city_stats_csv = collection_to_csv(&city_stats_rows)?;
    File::create(&city_stats_path)?.write_all(city_stats_csv.as_bytes())?;
    info!("Exported city stats to: {}", city_stats_path.display());

    // Write city_weights.csv
    let city_weights_rows: Vec<CityWeightRow> = output
        .inputs
        .city_weights
        .iter()
        .map(|(city, weight)| CityWeightRow {
            city: city.clone(),
            weight: *weight,
        })
        .collect();
    let city_weights_path = dir.join(format!("city-weights-epoch-{epoch}.csv"));
    let city_weights_csv = collection_to_csv(&city_weights_rows)?;
    File::create(&city_weights_path)?.write_all(city_weights_csv.as_bytes())?;
    info!("Exported city weights to: {}", city_weights_path.display());

    // Write shapley_settings.csv
    let settings_rows = vec![ShapleySettingsRow {
        operator_uptime: output.shapley_settings.operator_uptime,
        contiguity_bonus: output.shapley_settings.contiguity_bonus,
        demand_multiplier: output.shapley_settings.demand_multiplier,
    }];
    let settings_path = dir.join(format!("shapley-settings-epoch-{epoch}.csv"));
    let settings_csv = collection_to_csv(&settings_rows)?;
    File::create(&settings_path)?.write_all(settings_csv.as_bytes())?;
    info!("Exported shapley settings to: {}", settings_path.display());

    // Write per-city shapley values
    let per_city_rows: Vec<PerCityShapleyRow> = output
        .per_city_values
        .iter()
        .flat_map(|(city, values)| {
            values.iter().map(|v| PerCityShapleyRow {
                city: city.clone(),
                operator: v.operator.clone(),
                shapley_value: v.value,
            })
        })
        .collect();
    let per_city_path = dir.join(format!("per-city-shapley-epoch-{epoch}.csv"));
    let per_city_csv = collection_to_csv(&per_city_rows)?;
    File::create(&per_city_path)?.write_all(per_city_csv.as_bytes())?;
    info!("Exported per-city values to: {}", per_city_path.display());

    // Write aggregated shapley values
    let aggregated_path = dir.join(format!("aggregated-shapley-epoch-{epoch}.csv"));
    let aggregated_csv = collection_to_csv(&output.aggregated_output)?;
    File::create(&aggregated_path)?.write_all(aggregated_csv.as_bytes())?;
    info!(
        "Exported aggregated values to: {}",
        aggregated_path.display()
    );

    // Write info.csv with Solana epoch and summary statistics
    let info_rows = vec![InfoRow {
        doublezero_epoch: epoch,
        solana_epoch: solana_epoch.unwrap_or(0),
        cities_processed: output.per_city_values.len(),
        operators: output.aggregated_output.len(),
        devices: output.inputs.devices.len(),
        private_links: output.inputs.private_links.len(),
        public_links: output.inputs.public_links.len(),
        demands: output.inputs.demands.len(),
    }];
    let info_path = dir.join(format!("info-epoch-{epoch}.csv"));
    let info_csv = collection_to_csv(&info_rows)?;
    File::create(&info_path)?.write_all(info_csv.as_bytes())?;
    info!("Exported info to: {}", info_path.display());

    Ok(())
}

/// CSV row for city statistics
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
struct CityStatRow {
    city: String,
    validator_count: usize,
    total_stake_proxy: usize,
}

/// CSV row for city weights
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
struct CityWeightRow {
    city: String,
    weight: f64,
}

/// CSV row for shapley settings
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
struct ShapleySettingsRow {
    operator_uptime: f64,
    contiguity_bonus: f64,
    demand_multiplier: f64,
}

/// CSV row for info information
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
struct InfoRow {
    doublezero_epoch: u64,
    solana_epoch: u64,
    cities_processed: usize,
    operators: usize,
    devices: usize,
    private_links: usize,
    public_links: usize,
    demands: usize,
}
