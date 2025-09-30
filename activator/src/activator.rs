use crate::processor::Processor;
use doublezero_cli::{checkversion::check_version, doublezerocommand::CliCommandImpl};
use doublezero_sdk::{AccountData, DZClient, ProgramVersion};
use log::{error, info};
use solana_sdk::pubkey::Pubkey;
use std::{
    path::PathBuf,
    sync::{atomic::AtomicBool, Arc},
};
use tokio::sync::mpsc;

pub async fn run_activator(
    rpc_url: Option<String>,
    websocket_url: Option<String>,
    program_id: Option<String>,
    keypair: Option<PathBuf>,
) -> eyre::Result<()> {
    let client = create_client(rpc_url, websocket_url, program_id, keypair)?;
    version_check(&client)?;

    let (tx, rx) = mpsc::channel(128);
    let mut processor = Processor::new(rx, Arc::clone(&client))?;

    let shutdown = Arc::new(AtomicBool::new(false));

    // run on the tokio blocking thread pool so we can continue to run the metrics submitter in this async task
    let shutdown_clone = shutdown.clone();
    let client_clone = Arc::clone(&client);
    let tx_clone = tx.clone();
    let activator_handle = tokio::task::spawn_blocking(move || {
        info!("Activator thread started");
        process_events_thread(client_clone, tx_clone, shutdown_clone).unwrap_or_default()
    });

    let shutdown_clone = shutdown.clone();
    let client_clone = Arc::clone(&client);
    let accesspass_monitor_handle = tokio::task::spawn_blocking(move || {
        info!("User monitor thread started");
        crate::accesspass_monitor::process_access_pass_monitor_thread(client_clone, shutdown_clone)
            .unwrap_or_default()
    });

    tokio::select! {
        biased;
        _ = crate::listen_for_shutdown()? => {
            shutdown.store(true, std::sync::atomic::Ordering::Relaxed);
        }
        activator_res = activator_handle => {
            if let Err(err) = activator_res {
                error!("Activator thread exited unexpectedly with reason: {err:?}");
            }
        }
        accesspass_monitor_res = accesspass_monitor_handle => {
            if let Err(err) = accesspass_monitor_res {
                error!("AccessPass monitor thread exited unexpectedly with reason: {err:?}");
            }
        }
        snapshot_poll_res = get_snapshot_poll(Arc::clone(&client), tx, shutdown.clone()) => {
            if let Err(err) = snapshot_poll_res {
                error!("Snapshot poll exited unexpectedly with reason: {err:?}");
            }
        }
        _ = processor.run(shutdown.clone()) => {}
    }

    info!("Activator handler finished");
    Ok(())
}

fn create_client(
    rpc_url: Option<String>,
    websocket_url: Option<String>,
    program_id: Option<String>,
    keypair: Option<PathBuf>,
) -> eyre::Result<Arc<DZClient>> {
    let client = DZClient::new(rpc_url, websocket_url, program_id, keypair)?;

    info!(
        "Connected to RPC url: {} ws: {} program_id: {} ",
        client.get_rpc(),
        client.get_ws(),
        client.get_program_id()
    );

    Ok(Arc::new(client))
}

fn version_check(client: &DZClient) -> eyre::Result<()> {
    let stdout = std::io::stdout();
    let mut handle = stdout.lock();

    let cli = CliCommandImpl::new(client);
    check_version(&cli, &mut handle, ProgramVersion::current())?;
    Ok(())
}

pub async fn get_snapshot_poll(
    client: Arc<DZClient>,
    tx: mpsc::Sender<(Box<Pubkey>, Box<AccountData>, bool)>,
    stop_signal: Arc<AtomicBool>,
) -> eyre::Result<()> {
    while !stop_signal.load(std::sync::atomic::Ordering::Relaxed) {
        tokio::time::sleep(std::time::Duration::from_secs(60)).await;
        for (pubkey, data) in client.get_all()? {
            tx.send((pubkey, data, false)).await?;
        }
    }
    Ok(())
}

pub fn process_events_thread(
    client: Arc<DZClient>,
    tx: mpsc::Sender<(Box<Pubkey>, Box<AccountData>, bool)>,
    stop_signal: Arc<AtomicBool>,
) -> eyre::Result<()> {
    client.subscribe(
        |_, pubkey, data| {
            tx.blocking_send((pubkey, data, true))
                .unwrap_or_else(|err| {
                    log::error!("Failed to send websocket data to processor: {}", err);
                });
        },
        stop_signal,
    )?;
    Ok(())
}
