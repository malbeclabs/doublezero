use clap::Parser;
use doublezero_sdk::ProgramVersion;
use futures::{future::LocalBoxFuture, FutureExt};
use log::{error, info, LevelFilter};
use metrics_exporter_prometheus::PrometheusBuilder;
use std::{
    path::PathBuf,
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc,
    },
};
use tokio::signal;

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
#[command(name = "DoubleZero Activator")]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero")]
struct AppArgs {
    #[arg(long)]
    rpc: Option<String>,

    #[arg(long)]
    ws: Option<String>,

    #[arg(long)]
    program_id: Option<String>,

    #[arg(long)]
    keypair: Option<PathBuf>,

    #[arg(long)]
    influxdb_url: Option<String>,

    #[arg(long)]
    influxdb_org: Option<String>,

    #[arg(long)]
    influxdb_token: Option<String>,

    #[arg(long)]
    influxdb_bucket: Option<String>,

    #[arg(long, default_value = "warn")]
    log_level: String,
}

#[tokio::main]
async fn main() -> eyre::Result<()> {
    let args = AppArgs::parse();
    init_logger(&args.log_level);

    PrometheusBuilder::new().install()?;

    export_build_info();

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
    let shutdown = Arc::new(AtomicBool::new(false));
    let shutdown_clone = shutdown.clone();

    activator.init().await?;

    info!("Initialized");

    // run on the tokio blocking thread pool so we can continue to run the metrics submitter in this async task
    let activator_handle = tokio::task::spawn_blocking(move || {
        info!("Activator thread started");
        activator.run(shutdown_clone).unwrap_or_default()
    });

    info!("Activator metrics submitter started");

    tokio::select! {
        biased;
        _ = listen_for_shutdown()? => {
            shutdown.store(true, Ordering::Relaxed);
        }
        activator_res = activator_handle => {
            if let Err(err) = activator_res {
                error!("Activator thread exited unexpectedly with reason: {err:?}");
            }
        }
        _ = metrics_submitter.run(shutdown.clone()) => {}
    }

    info!("Activator handler finished");
    info!("Activator metrics submitter finished");

    Ok(())
}

fn listen_for_shutdown() -> eyre::Result<LocalBoxFuture<'static, ()>> {
    let mut sigterm = signal::unix::signal(signal::unix::SignalKind::terminate())?;
    let shutdown = Box::pin(
        futures::future::select(
            Box::pin(async move { sigterm.recv().await }),
            Box::pin(signal::ctrl_c()),
        )
        .map(|_| ()),
    );
    Ok(shutdown)
}

fn init_logger(log_level: &str) {
    let log_level = match log_level.to_lowercase().as_str() {
        "trace" => LevelFilter::Trace,
        "debug" => LevelFilter::Debug,
        "info" => LevelFilter::Info,
        "warn" => LevelFilter::Warn,
        "error" => LevelFilter::Error,
        _ => {
            eprintln!("Invalid log level: {log_level}. Using default 'warn'");
            LevelFilter::Warn
        }
    };

    env_logger::Builder::new().filter_level(log_level).init();
}

fn export_build_info() {
    let version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION"));
    let build_commit = option_env!("BUILD_COMMIT").unwrap_or("unknown");
    let build_date = option_env!("DATE").unwrap_or("unknown");
    let pkg_version = env!("CARGO_PKG_VERSION");
    let program_version = ProgramVersion::current().to_string();

    metrics::gauge!(
        "doublezero_activator_build_info",
        "version" => version,
        "commit" => build_commit,
        "date" => build_date,
        "pkg_version" => pkg_version,
        "program_version" => program_version
    )
    .set(1);
}
