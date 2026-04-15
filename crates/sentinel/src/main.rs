use clap::Parser;
use doublezero_sentinel::{multicast_publisher::MulticastPublisherSentinel, settings::AppArgs};
use metrics_exporter_prometheus::PrometheusBuilder;
use solana_sdk::signer::Signer;
use tokio::signal;
use tokio_util::sync::CancellationToken;
use tracing::{error, info};
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = AppArgs::parse();

    tracing_subscriber::registry()
        .with(tracing_subscriber::EnvFilter::new(&args.log))
        .with(tracing_subscriber::fmt::layer())
        .init();

    PrometheusBuilder::new()
        .with_http_listener(args.metrics_addr())
        .install()?;

    export_build_info();

    let dz_rpc_url = args.dz_rpc_url();
    let solana_rpc_url = args.solana_rpc_url();
    let keypair = args.keypair();
    let serviceability_id = args.serviceability_program_id()?;
    let multicast_group_pubkeys = args.multicast_group_pubkeys()?;

    info!(
        %dz_rpc_url,
        %solana_rpc_url,
        poll_interval_secs = args.poll_interval,
        pubkey = %keypair.pubkey(),
        groups = ?multicast_group_pubkeys,
        client_filter = ?args.client_filter,
        "DoubleZero Sentinel starting"
    );

    let sentinel = MulticastPublisherSentinel::new(
        dz_rpc_url,
        solana_rpc_url,
        keypair,
        serviceability_id,
        multicast_group_pubkeys,
        args.client_filter,
        args.validator_metadata_url,
        args.poll_interval,
    );

    let shutdown_listener = shutdown_listener();

    tokio::select! {
        biased;
        _ = shutdown_listener.cancelled() => {
            info!("shutdown signal received");
        },
        result = sentinel.run(shutdown_listener.clone()) => {
            if let Err(err) = result {
                error!(?err, "multicast publisher sentinel exited with error");
            }
        }
    }

    info!("DoubleZero Sentinel shutting down");

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
