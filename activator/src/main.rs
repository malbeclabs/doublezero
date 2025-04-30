use clap::Parser;
use std::thread;

mod activator;
mod activator_metrics;
mod idallocator;
mod influxdb_metrics_service;
mod ipblockallocator;
mod metrics_service;
mod states;
mod utils;
include!(concat!(env!("OUT_DIR"), "/version.rs"));

#[derive(Parser, Debug)]
#[command(term_width = 0)]
#[command(name = "Doublezero activator")]
#[command(version = "1.0")]
#[command(about = "Double Zero ", long_about = None)]
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
    let args = AppArgs::parse();

    println!("DoubleZero Activator {}", APP_VERSION);

    let (metrics_service, mut metrics_submitter) =
        influxdb_metrics_service::create_influxdb_metrics_service(
            args.influxdb_url.as_deref(),
            args.influxdb_org.as_deref(),
            args.influxdb_token.as_deref(),
            args.influxdb_bucket.as_deref(),
        );

    let mut activator = activator::Activator::new(
        args.rpc,
        args.ws,
        args.program_id,
        args.keypair,
        metrics_service,
    )
    .await
    .unwrap();

    println!("Activator started");

    activator.init().await?;

    println!("Initialized");

    // background blocking code so we can continue to run the metrics submitter in this async task
    thread::spawn(move || {
        println!("Acivator thread started");
        activator.run().unwrap_or_default()
    });

    println!("Activator metrics submitter started");
    metrics_submitter.run().await;
    println!("Activator metrics submitter finished");

    Ok(())
}
