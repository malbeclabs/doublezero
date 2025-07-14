use clap::Parser;
use log::info;
use std::thread;

mod activator;
mod activator_metrics;
mod idallocator;
mod influxdb_metrics_service;
mod ipblockallocator;
mod metrics_service;
mod process;
mod states;
mod tenants;
pub mod tests;
mod utils;

#[derive(Parser, Debug)]
#[command(term_width = 0)]
#[command(name = "Doublezero Activator")]
#[command(version = env!("CARGO_PKG_VERSION"))]
#[command(about = "Double Zero")]
struct AppArgs {
    #[arg(long)]
    rpc: Option<String>,

    #[arg(long)]
    ws: Option<String>,

    #[arg(long)]
    program_id: Option<String>,

    #[arg(long)]
    keypair: Option<String>,

    #[arg(long)]
    influxdb_url: Option<String>,

    #[arg(long)]
    influxdb_org: Option<String>,

    #[arg(long)]
    influxdb_token: Option<String>,

    #[arg(long)]
    influxdb_bucket: Option<String>,
}

#[tokio::main]
async fn main() -> eyre::Result<()> {
    env_logger::init();
    let args = AppArgs::parse();

    info!("DoubleZero Activator");

    let (metrics_service, mut metrics_submitter) =
        influxdb_metrics_service::create_influxdb_metrics_service(
            args.influxdb_url.as_deref(),
            args.influxdb_org.as_deref(),
            args.influxdb_token.as_deref(),
            args.influxdb_bucket.as_deref(),
        )?;

    let mut activator = activator::Activator::new(
        args.rpc,
        args.ws,
        args.program_id,
        args.keypair,
        metrics_service,
    )
    .await?;

    info!("Activator started");

    activator.init().await?;

    info!("Initialized");

    // background blocking code so we can continue to run the metrics submitter in this async task
    thread::spawn(move || {
        info!("Acivator thread started");
        activator.run().unwrap_or_default()
    });

    info!("Activator metrics submitter started");
    metrics_submitter.run().await;
    info!("Activator metrics submitter finished");

    Ok(())
}
