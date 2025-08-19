use clap::Parser;
use doublezero_ledger_sentinel::{
    sentinel::{ReqListener, Sentinel},
    settings::{AppArgs, Settings},
};
use metrics_exporter_prometheus::PrometheusBuilder;
use solana_sdk::signer::Signer;
use tokio::signal;
use tokio_util::sync::CancellationToken;
use tracing::{error, info};
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = AppArgs::parse();
    let settings = Settings::new(args.config)?;

    tracing_subscriber::registry()
        .with(tracing_subscriber::EnvFilter::new(&settings.log))
        .with(tracing_subscriber::fmt::layer())
        .init();

    PrometheusBuilder::new()
        .with_http_listener(settings.metrics_addr())
        .install()?;

    export_build_info();

    let sol_rpc = settings.sol_rpc();
    let sol_ws = settings.sol_ws();
    let dz_rpc = settings.dz_rpc();
    let keypair = settings.keypair();

    info!(
        %sol_rpc,
        %sol_ws,
        %dz_rpc,
        pubkey = %keypair.pubkey(),
        "DoubleZero Ledger Sentinel starting"
    );

    let (request_listener, rx) = ReqListener::new(sol_ws).await?;
    let mut sentinel = Sentinel::new(
        dz_rpc,
        sol_rpc,
        keypair,
        rx,
        settings.onboarding_lamports(),
        settings.previous_leader_epochs(),
    )
    .await?;

    let shutdown_listener = shutdown_listener();

    tokio::select! {
        biased;
        _ = shutdown_listener.cancelled() => {
            info!("shutdown signal received");
        },
        result = request_listener.run(shutdown_listener.clone()) => {
            if let Err(err) = result {
                error!(?err, "sentinel request listener exited with error");
            }
        }
        result = sentinel.run(shutdown_listener.clone()) => {
            if let Err(err) = result {
                error!(?err, "sentinel handler exited with error");
            }
        }
    }

    info!("DoubleZero Ledger Sentinel shutting down");

    Ok(())
}

fn shutdown_listener() -> CancellationToken {
    let cancellation_token = CancellationToken::new();
    let mut sigterm = signal::unix::signal(signal::unix::SignalKind::terminate())
        .expect("sigterm listener failed");
    tokio::spawn({
        let cancellation_token = cancellation_token.clone();
        async move {
            tokio::select! {
                _ = sigterm.recv() => cancellation_token.cancel(),
                _ = signal::ctrl_c() => cancellation_token.cancel(),
            }
        }
    });

    cancellation_token
}

fn export_build_info() {
    let version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION"));
    let build_commit = option_env!("BUILD_COMMIT").unwrap_or("UNKNOWN");
    let build_date = option_env!("DATE").unwrap_or("UNKNOWN");
    let pkg_version = env!("CARGO_PKG_VERSION");

    metrics::gauge!(
        "doublezero_sentinel_build_info",
        "version" => version,
        "commit" => build_commit,
        "date" => build_date,
        "pkg_version" => pkg_version
    )
    .set(1);
}
