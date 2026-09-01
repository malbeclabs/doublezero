use std::{
    env,
    net::{IpAddr, SocketAddr},
    str::FromStr,
};

use anyhow::Result;
use clap::Parser;
use doublezero_solana_validator_debt::command::ValidatorDebtCommand;
use metrics_exporter_prometheus::PrometheusBuilder;
use tracing::{debug, warn};
use tracing_subscriber::{EnvFilter, layer::SubscriberExt, util::SubscriberInitExt};

#[derive(Debug, Parser)]
#[command(term_width = 0)]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero Solana Debt Calculation Commands", long_about = None)]
struct ValidatorDebtApp {
    #[command(subcommand)]
    command: ValidatorDebtCommand,
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::registry()
        .with(EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")))
        .with(
            tracing_subscriber::fmt::layer()
                .with_target(false)
                .with_thread_ids(false)
                .with_thread_names(false),
        )
        .init();

    if let Some(socket) = metrics_addr() {
        if let Err(e) = PrometheusBuilder::new()
            .with_http_listener(socket)
            .install()
        {
            warn!("Failed to initialize metrics exporter: {e}. Continuing without metrics.");
        } else {
            export_build_info();
            debug!("Metrics exporter initialized on {}", socket);
        };
    }

    ValidatorDebtApp::parse().command.try_into_execute().await
}

fn export_build_info() {
    let version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION"));
    let build_commit = option_env!("BUILD_COMMIT").unwrap_or("UNKNOWN");
    let build_date = option_env!("DATE").unwrap_or("UNKNOWN");
    let pkg_version = env!("CARGO_PKG_VERSION");

    metrics::gauge!(
        "doublezero_validator_debt_build_info",
        "version" => version,
        "commit" => build_commit,
        "date" => build_date,
        "pkg_version" => pkg_version
    )
    .set(1.0);
}

fn metrics_addr() -> Option<SocketAddr> {
    env::var("VALIDATOR_DEBT_METRICS_ADDR")
        .ok()
        .and_then(|addr_str| IpAddr::from_str(&addr_str).ok())
        .map(|ip_addr| SocketAddr::new(ip_addr, 9090))
}
