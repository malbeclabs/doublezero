use crate::cli::{
    common::{OutputFormat, OutputOptions, to_json_string},
    traits::Exportable,
};
use anyhow::{Result, bail};
use clap::Subcommand;
use contributor_rewards::{
    calculator::{
        orchestrator::Orchestrator,
        shapley_handler::{
            PreviousEpochCache, build_devices, build_private_links, build_public_links,
        },
    },
    ingestor::{demand, fetcher::Fetcher},
    processor::{internet::InternetTelemetryProcessor, telemetry::DZDTelemetryProcessor},
};
use network_shapley::types::{Demand, Demands, Devices, PrivateLinks, PublicLinks};
use solana_sdk::pubkey::Pubkey;
use std::{collections::BTreeSet, path::PathBuf};
use tracing::{info, warn};

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

    # Export ShapleyInputs to JSON
    inspect shapley --epoch 9 --skip-users --output-format json-pretty --output-dir ./debug/"#
    )]
    Shapley {
        /// DZ epoch to debug
        #[arg(short, long, value_name = "EPOCH")]
        epoch: Option<u64>,

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
            skip_users,
            use_test_demands,
            output_format,
            output_dir,
            output_file,
        } => {
            handle_inspect_shapley(
                orchestrator,
                epoch,
                skip_users,
                use_test_demands,
                output_format,
                output_dir,
                output_file,
            )
            .await
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
    epoch: Option<u64>,
    skip_users: bool,
    use_test_demands: bool,
    output_format: OutputFormat,
    output_dir: Option<PathBuf>,
    output_file: Option<PathBuf>,
) -> Result<()> {
    info!(
        "Debugging Shapley calculations with {} demands",
        if use_test_demands { "test" } else { "real" }
    );

    // Fetch data
    let fetcher = Fetcher::from_settings(orchestrator.settings())?;
    let (fetch_epoch, fetch_data) = match epoch {
        Some(e) => fetcher.with_epoch(e).await?,
        None => fetcher.fetch().await?,
    };

    info!("Using data from epoch {}", fetch_epoch);

    // Check for users if not skipping
    if !skip_users && fetch_data.dz_serviceability.users.is_empty() {
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

    let devices = build_devices(&fetch_data)?;
    let private_links = build_private_links(
        orchestrator.settings(),
        &fetch_data,
        &dzd_stats,
        &previous_epoch_cache,
    );
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
    let demands = if use_test_demands {
        info!("Using uniform test demands for debugging");
        generate_uniform_test_demands(&cities_vec)?
    } else {
        info!("Fetching real leader schedule from Solana");
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
        is_test_data: use_test_demands,
        devices,
        private_links,
        public_links,
        demands,
        cities: cities_vec.clone(),
    };

    // Export results
    let output_options = OutputOptions {
        output_format,
        output_dir: output_dir.map(|p| p.to_string_lossy().to_string()),
        output_file: output_file.map(|p| p.to_string_lossy().to_string()),
    };

    let default_filename = format!(
        "shapley-debug-{}-epoch-{fetch_epoch}",
        if use_test_demands { "test" } else { "real" }
    );
    output_options.write(&shapley_inputs, &default_filename)?;

    // Print summary
    println!("\nShapley Debug Summary:");
    println!("----------------------");
    println!("Epoch: {fetch_epoch}");
    println!(
        "Demands: {}",
        if use_test_demands { "Test" } else { "Real" }
    );
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
