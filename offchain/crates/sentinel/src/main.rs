use clap::Parser;
use doublezero_ledger_sentinel::{
    constants::ENV_PREVIOUS_LEADER_EPOCHS,
    sentinel::PollingSentinel,
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

    let sol_rpc_url = settings.sol_rpc();
    let dz_rpc_url = settings.dz_rpc();
    let keypair = settings.keypair();
    let serviceability_id = settings.serviceability_program_id()?;

    info!(
        %sol_rpc_url,
        %dz_rpc_url,
        poll_interval_secs = args.poll_interval,
        pubkey = %keypair.pubkey(),
        "DoubleZero Ledger Sentinel starting"
    );

    let mut polling_sentinel = PollingSentinel::new(
        dz_rpc_url,
        sol_rpc_url,
        keypair,
        serviceability_id,
        args.poll_interval,
        ENV_PREVIOUS_LEADER_EPOCHS,
    )
    .await?;

    let shutdown_listener = shutdown_listener();

    tokio::select! {
        biased;
        _ = shutdown_listener.cancelled() => {
            info!("shutdown signal received");
        },
        result = polling_sentinel.run(shutdown_listener.clone()) => {
            if let Err(err) = result {
                error!(?err, "polling sentinel exited with error");
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
