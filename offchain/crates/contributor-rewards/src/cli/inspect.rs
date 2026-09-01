use std::{collections::BTreeSet, path::PathBuf};

use anyhow::{Result, bail};
use clap::Subcommand;
use network_shapley::types::{Demand, Demands, Devices, PrivateLinks, PublicLinks};
use solana_sdk::pubkey::Pubkey;
use tracing::{info, warn};

use crate::{
    calculator::{
        orchestrator::Orchestrator,
        shapley::handler::{
            PreviousEpochCache, build_devices, build_private_links, build_public_links,
        },
    },
    cli::{
        common::{OutputFormat, OutputOptions, to_json_string},
        snapshot::CompleteSnapshot,
        traits::Exportable,
    },
    ingestor::{demand, fetcher::Fetcher},
    processor::{internet::InternetTelemetryProcessor, telemetry::DZDTelemetryProcessor},
};

/// Inspect commands for analyzing rewards and Shapley calculations
#[derive(Subcommand, Debug)]
pub enum InspectCommands {
    #[command(
        about = "Inspect and display information about reward record accounts for an epoch",
        after_help = r#"Examples:
    # Inspect all records for epoch 123
    inspect rewards --epoch 123

    # Inspect only device telemetry records
    inspect rewards --epoch 123 --type device-telemetry

    # Inspect with specific rewards accountant
    inspect rewards --epoch 123 --rewards-accountant <PUBKEY>"#
    )]
    Rewards {
        /// DZ epoch number to inspect records for
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// Rewards accountant public key (auto-fetched from ProgramConfig if not provided)
        #[arg(short = 'r', long, value_name = "PUBKEY")]
        rewards_accountant: Option<Pubkey>,

        /// Specific record type to inspect (shows all if not specified)
        #[arg(short = 't', long, value_name = "TYPE")]
        r#type: Option<String>,
    },

    #[command(
        about = "Debug and analyze Shapley calculations with real or test demands",
        after_help = r#"Examples:
    # Debug with real leader schedule (skip user check)
    inspect shapley --epoch 9 --skip-users

    # Use test demands for debugging
    inspect shapley --epoch 9 --use-test-demands

    # Use snapshot for historical epochs (loads all data from snapshot)
    inspect shapley -s mn-epoch-46-snapshot.json

    # Export ShapleyInputs to JSON
    inspect shapley --epoch 9 --skip-users --output-format json-pretty --output-dir ./debug/"#
    )]
    Shapley {
        /// DZ epoch to debug
        #[arg(short, long, value_name = "EPOCH")]
        epoch: Option<u64>,

        /// Path to snapshot file (loads all data from snapshot, mutually exclusive with --epoch)
        #[arg(short = 's', long, value_name = "FILE")]
        snapshot: Option<PathBuf>,

        /// Skip serviceability user requirement check
        #[arg(long)]
        skip_users: bool,

        /// Use uniform test demands instead of real leader schedule
        #[arg(long)]
        use_test_demands: bool,

        /// Output format for exports
        #[arg(short = 'f', long, default_value = "json-pretty")]
        output_format: OutputFormat,

        /// Directory to export files
        #[arg(short = 'o', long, value_name = "DIR")]
        output_dir: Option<PathBuf>,

        /// Specific output file path
        #[arg(long, value_name = "FILE")]
        output_file: Option<PathBuf>,
    },
}

/// Arguments for shapley inspection
struct ShapleyInspectArgs {
    epoch: Option<u64>,
    snapshot: Option<PathBuf>,
    skip_users: bool,
    use_test_demands: bool,
    output_format: OutputFormat,
    output_dir: Option<PathBuf>,
    output_file: Option<PathBuf>,
}

/// Container for Shapley inputs using existing types
#[derive(Debug, serde::Serialize, serde::Deserialize)]
pub struct ShapleyInputs {
    pub epoch: u64,
    pub is_test_data: bool,
    pub devices: Devices,
    pub private_links: PrivateLinks,
    pub public_links: PublicLinks,
    pub demands: Demands,
    pub cities: Vec<String>,
}

impl Exportable for ShapleyInputs {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => {
                bail!(
                    "CSV export not supported for complex Shapley inputs. Use JSON format instead."
                )
            }
            OutputFormat::Json => to_json_string(self, false),
            OutputFormat::JsonPretty => to_json_string(self, true),
        }
    }
}

/// Handle inspect commands
pub async fn handle(orchestrator: &Orchestrator, cmd: InspectCommands) -> Result<()> {
    match cmd {
        InspectCommands::Rewards {
            epoch,
            rewards_accountant,
            r#type,
        } => handle_inspect_rewards(orchestrator, epoch, rewards_accountant, r#type).await,
        InspectCommands::Shapley {
            epoch,
            snapshot,
            skip_users,
            use_test_demands,
            output_format,
            output_dir,
            output_file,
        } => {
            let args = ShapleyInspectArgs {
                epoch,
                snapshot,
                skip_users,
                use_test_demands,
                output_format,
                output_dir,
                output_file,
            };
            handle_inspect_shapley(orchestrator, args).await
        }
    }
}

async fn handle_inspect_rewards(
    orchestrator: &Orchestrator,
    epoch: u64,
    rewards_accountant: Option<Pubkey>,
    r#type: Option<String>,
) -> Result<()> {
    orchestrator
        .inspect_records(epoch, rewards_accountant, r#type)
        .await
}

async fn handle_inspect_shapley(
    orchestrator: &Orchestrator,
    args: ShapleyInspectArgs,
) -> Result<()> {
    // Validate conflicting flags
    if args.use_test_demands && args.snapshot.is_some() {
        bail!("Cannot use both --use-test-demands and --snapshot together");
    }
    if args.epoch.is_some() && args.snapshot.is_some() {
        bail!(
            "Cannot use both --epoch and --snapshot together. The snapshot contains its own epoch."
        );
    }

    let demand_source = if args.use_test_demands {
        "test"
    } else if args.snapshot.is_some() {
        "snapshot"
    } else {
        "real"
    };
    info!(
        "Debugging Shapley calculations with {} demands",
        demand_source
    );

    // Load data from snapshot or fetch from network
    let (fetch_epoch, fetch_data, snapshot_leader_schedule) =
        if let Some(ref snapshot_path) = args.snapshot {
            info!("Loading all data from snapshot: {:?}", snapshot_path);
            let loaded_snapshot = CompleteSnapshot::load_from_file(snapshot_path)?;
            let leader_schedule = loaded_snapshot.leader_schedule.ok_or_else(|| {
                anyhow::anyhow!("Snapshot {:?} missing leader schedule", snapshot_path)
            })?;
            info!(
                "Loaded snapshot for DZ epoch {} (Solana epoch: {})",
                loaded_snapshot.dz_epoch, leader_schedule.solana_epoch
            );
            (
                loaded_snapshot.dz_epoch,
                loaded_snapshot.fetch_data,
                Some(leader_schedule),
            )
        } else {
            let fetcher = Fetcher::from_settings(orchestrator.settings())?;
            let (epoch, data) = fetcher.fetch(args.epoch).await?;
            (epoch, data, None)
        };

    info!("Using data from epoch {}", fetch_epoch);

    // Check for users if not skipping
    if !args.skip_users && fetch_data.dz_serviceability.users.is_empty() {
        warn!("No users found in serviceability data!");
        bail!(
            "No users found. Use --skip-users to proceed anyway or --use-test-demands for testing."
        );
    }

    // Process telemetry
    let dzd_stats = DZDTelemetryProcessor::process(&fetch_data)?;
    let internet_stats = InternetTelemetryProcessor::process(&fetch_data)?;

    info!(
        "Processed {} device links and {} internet links",
        dzd_stats.len(),
        internet_stats.len()
    );

    // Build Shapley inputs using existing types
    // Create an empty cache since we're just inspecting, not applying defaults
    let previous_epoch_cache = PreviousEpochCache::new();

    let (devices, device_ids) = build_devices(&fetch_data, &orchestrator.settings().network)?;
    let private_links = build_private_links(&fetch_data, &device_ids);
    let public_links = build_public_links(
        orchestrator.settings(),
        &internet_stats,
        &fetch_data,
        &previous_epoch_cache,
    )?;

    // Get unique cities from public links
    let mut cities = BTreeSet::new();
    for link in &public_links {
        cities.insert(link.city1.clone());
        cities.insert(link.city2.clone());
    }
    let cities_vec: Vec<String> = cities.into_iter().collect();

    info!("Found {} unique cities", cities_vec.len());

    // Generate demands
    let demands = if args.use_test_demands {
        info!("Using uniform test demands for debugging");
        generate_uniform_test_demands(&cities_vec)?
    } else if let Some(leader_schedule) = snapshot_leader_schedule {
        info!(
            "Using leader schedule from snapshot (Solana epoch: {})",
            leader_schedule.solana_epoch
        );
        let demand_output =
            demand::build_with_schedule(orchestrator.settings(), &fetch_data, &leader_schedule)?;
        info!(
            "Generated {} demands from {} cities with validators",
            demand_output.demands.len(),
            demand_output.city_stats.len()
        );
        demand_output.demands
    } else {
        info!("Fetching real leader schedule from Solana");
        let fetcher = Fetcher::from_settings(orchestrator.settings())?;
        let demand_output = demand::build(&fetcher, &fetch_data).await?;
        info!(
            "Generated {} real demands from {} cities with validators",
            demand_output.demands.len(),
            demand_output.city_stats.len()
        );
        demand_output.demands
    };

    info!("Generated {} demand pairs", demands.len());

    // Create export structure using existing types
    let shapley_inputs = ShapleyInputs {
        epoch: fetch_epoch,
        is_test_data: args.use_test_demands,
        devices,
        private_links,
        public_links,
        demands,
        cities: cities_vec.clone(),
    };

    // Export results
    let output_options = OutputOptions {
        output_format: args.output_format,
        output_dir: args.output_dir.map(|p| p.to_string_lossy().to_string()),
        output_file: args.output_file.map(|p| p.to_string_lossy().to_string()),
    };

    let default_filename = format!("shapley-inputs-{demand_source}-epoch-{fetch_epoch}");
    output_options.write(&shapley_inputs, &default_filename)?;

    // Print summary
    println!("\nShapley Inputs Summary:");
    println!("----------------------");
    println!("Epoch: {fetch_epoch}");
    println!("Demand source: {demand_source}");
    println!("Cities: {}", shapley_inputs.cities.len());
    println!("Devices: {}", shapley_inputs.devices.len());
    println!("Private Links: {}", shapley_inputs.private_links.len());
    println!("Public Links: {}", shapley_inputs.public_links.len());
    println!("Demands: {}", shapley_inputs.demands.len());

    Ok(())
}

/// Generate uniform test demands for debugging - equal traffic between all city pairs
fn generate_uniform_test_demands(cities: &[String]) -> Result<Demands> {
    let mut demands = Vec::new();
    let mut demand_type = 1u32;

    for source in cities {
        for destination in cities {
            if source != destination {
                demands.push(Demand::new(
                    source.clone(),
                    destination.clone(),
                    1,   // receivers
                    1.0, // uniform traffic
                    1.0, // uniform priority
                    demand_type,
                    false, // no multicast for test
                ));
            }
        }
        demand_type += 1;
    }

    info!(
        "Generated {} uniform test demands across {} cities",
        demands.len(),
        cities.len()
    );
    Ok(demands)
}
