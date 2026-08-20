use anyhow::Context;
use clap::Parser;
use doublezero_ip_verifier::{
    epoch::{run_refresher, LedgerEpochSource},
    server::{router, AppState},
    settings::AppArgs,
};
use metrics_exporter_prometheus::PrometheusBuilder;
use solana_signer::Signer;
use std::{net::SocketAddr, sync::Arc, time::Duration};
use tokio::signal;
use tokio_util::sync::CancellationToken;
use tracing::info;
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = AppArgs::parse();

    tracing_subscriber::registry()
        .with(tracing_subscriber::EnvFilter::new(&args.log))
        .with(tracing_subscriber::fmt::layer())
        .init();

    PrometheusBuilder::new()
        .with_http_listener(args.metrics_addr)
        .install()
        .context("failed to install the Prometheus exporter")?;
    export_build_info();

    let verifier = args.keypair()?;
    let ledger_rpc_url = args.ledger_rpc_url()?;
    let epoch = Arc::new(args.epoch_cache());
    let limiter = Arc::new(args.rate_limiter());

    info!(
        %ledger_rpc_url,
        listen_addr = %args.listen_addr,
        metrics_addr = %args.metrics_addr,
        verifier_pubkey = %verifier.pubkey(),
        trusted_proxies = ?args.trusted_proxies,
        epoch_refresh_secs = args.epoch_refresh_secs,
        max_epoch_age_secs = args.max_epoch_age_secs,
        "DoubleZero IP verifier starting"
    );
    if args.trusted_proxies.is_empty() {
        info!(
            "no trusted proxies configured: forwarded headers are ignored and the connection peer \
             address is signed"
        );
    }

    let shutdown = shutdown_listener();

    let refresher = tokio::spawn(run_refresher(
        epoch.clone(),
        Arc::new(LedgerEpochSource::new(ledger_rpc_url)),
        Duration::from_secs(args.epoch_refresh_secs),
        shutdown.clone(),
    ));

    let state = AppState::new(verifier, epoch, limiter, args.trusted_proxies.clone());
    let listener = tokio::net::TcpListener::bind(args.listen_addr)
        .await
        .with_context(|| format!("failed to bind {}", args.listen_addr))?;

    // `into_make_service_with_connect_info` is what makes the peer address available to the
    // handler. Without it the proof endpoint has nothing to attest.
    axum::serve(
        listener,
        router(state, args.request_limits()).into_make_service_with_connect_info::<SocketAddr>(),
    )
    .with_graceful_shutdown(async move { shutdown.cancelled().await })
    .await
    .context("HTTP server failed")?;

    refresher.await.ok();
    info!("DoubleZero IP verifier shutting down");

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

    metrics::gauge!(
        "doublezero_ip_verifier_build_info",
        "version" => version,
        "commit" => build_commit,
        "date" => build_date,
    )
    .set(1);
}
