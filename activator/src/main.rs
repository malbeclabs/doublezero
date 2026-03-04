use clap::Parser;
use doublezero_config::Environment;
use doublezero_sdk::ProgramVersion;
use futures::{future::LocalBoxFuture, FutureExt};
use log::{info, LevelFilter};
use metrics_exporter_prometheus::PrometheusBuilder;
use std::path::PathBuf;
use tokio::signal;

mod activator;
mod activator_metrics;
mod idallocator;
mod ipblockallocator;
mod process;
mod processor;
mod states;
pub mod test_helpers;
pub mod tests;

#[derive(Parser, Debug)]
#[command(term_width = 0)]
#[command(name = "DoubleZero Activator")]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero")]
struct AppArgs {
    #[arg(long)]
    env: Option<String>,

    #[arg(long)]
    rpc: Option<String>,

    #[arg(long)]
    ws: Option<String>,

    #[arg(long)]
    program_id: Option<String>,

    #[arg(long)]
    keypair: Option<PathBuf>,

    #[arg(long, default_value = "info")]
    log_level: String,
}

#[tokio::main]
async fn main() -> eyre::Result<()> {
    let args = AppArgs::parse();
    init_logger(&args.log_level);

    PrometheusBuilder::new().install()?;

    export_build_info();

    info!("DoubleZero Activator");

    let (rpc_url, ws_url, program_id) = if let Some(env) = args.env {
        let config = env.parse::<Environment>()?.config()?;
        (
            config.ledger_public_rpc_url,
            config.ledger_public_ws_rpc_url,
            config.serviceability_program_id.to_string(),
        )
    } else {
        (
            args.rpc
                .ok_or_else(|| eyre::eyre!("RPC URL is required when env is not provided"))?,
            args.ws
                .ok_or_else(|| eyre::eyre!("WebSocket URL is required when env is not provided"))?,
            args.program_id
                .ok_or_else(|| eyre::eyre!("Program ID is required when env is not provided"))?,
        )
    };

    let keypair = args
        .keypair
        .ok_or_else(|| eyre::eyre!("Keypair is required"))?;

    activator::run_activator(
        Some(rpc_url.clone()),
        Some(ws_url.clone()),
        Some(program_id.clone()),
        Some(keypair.clone()),
    )
    .await?;

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
    let build_date = option_env!("BUILD_DATE").unwrap_or("unknown");
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
