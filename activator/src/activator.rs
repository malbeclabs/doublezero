use crate::{accesspass_monitor, processor::Processor};
use doublezero_cli::{checkversion::check_version, doublezerocommand::CliCommandImpl};
use doublezero_sdk::{
    rpckeyedaccount_decode::rpckeyedaccount_decode, AccountData, AsyncDZClient, DZClient,
    ProgramVersion,
};
use futures::stream::StreamExt;
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

    loop {
        info!("Activator handler loop started");

        let (tx, rx) = mpsc::channel(128);
        let mut processor = Processor::new(rx, Arc::clone(&client))?;

        let shutdown = Arc::new(AtomicBool::new(false));

        tokio::select! {
            biased;
            _ = crate::listen_for_shutdown()? => {
                info!("Shutdown signal received, stopping activator...");
                shutdown.store(true, std::sync::atomic::Ordering::Relaxed);
                break;
            }
            _ = websocket_task(Arc::clone(&client), tx.clone(), shutdown.clone()) => {
                info!("Websocket task finished, stopping activator...");
            }
            accesspass_monitor_res = accesspass_monitor::access_pass_monitor_task(Arc::clone(&client), shutdown.clone()) => {
                if let Err(err) = accesspass_monitor_res {
                    error!("AccessPass monitor task exited unexpectedly with reason: {err:?}");
                } else {
                    info!("AccessPass monitor task finished, stopping activator...");
                }
            }
            snapshot_poll_res = get_snapshot_poll(Arc::clone(&client), tx.clone(), shutdown.clone()) => {
                if let Err(err) = snapshot_poll_res {
                    error!("Snapshot poll exited unexpectedly with reason: {err:?}");
                }
                else {
                    info!("Snapshot poll task finished, stopping activator...");
                }
            }
            _ = processor.run(shutdown.clone()) => {
                info!("Processor task finished, stopping activator...");
            }
        }
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
    tx: mpsc::Sender<(Box<Pubkey>, Box<AccountData>)>,
    stop_signal: Arc<AtomicBool>,
) -> eyre::Result<()> {
    while !stop_signal.load(std::sync::atomic::Ordering::Relaxed) {
        for (pubkey, data) in client.get_all()? {
            tx.send((pubkey, data)).await?;
        }
        tokio::time::sleep(std::time::Duration::from_secs(60)).await;
    }
    Ok(())
}

pub async fn websocket_task(
    client: Arc<DZClient>,
    tx: mpsc::Sender<(Box<Pubkey>, Box<AccountData>)>,
    stop_signal: Arc<AtomicBool>,
) {
    while !stop_signal.load(std::sync::atomic::Ordering::Relaxed) {
        info!(
            "Starting websocket task on {}, program_id: {}",
            client.get_ws(),
            client.get_program_id()
        );
        match AsyncDZClient::new(client.get_ws(), *client.get_program_id()).await {
            Ok(async_client) => match async_client.subscribe().await {
                Ok((mut subscription, unsubscribe)) => {
                    info!("Websocket subscription established");
                    while !stop_signal.load(std::sync::atomic::Ordering::Relaxed) {
                        if let Some(msg) = subscription.next().await {
                            let keyed_account = msg.value;
                            let pubkey = keyed_account.pubkey.clone();
                            match rpckeyedaccount_decode(keyed_account) {
                                Ok(Some((pubkey, account))) => {
                                    tx.send((pubkey, account)).await.unwrap();
                                }
                                Ok(None) => {
                                    info!("Received account with empty data for pubkey {}", pubkey);
                                }
                                Err(e) => {
                                    error!(
                                        "Error parsing RpcKeyedAccount for pubkey {}: {e}",
                                        pubkey
                                    );
                                }
                            }
                        } else {
                            break;
                        }
                    }
                    unsubscribe().await;
                    info!("Websocket subscription ended gracefully");
                }
                Err(e) => {
                    error!("Failed to establish websocket subscription: {e}");
                }
            },
            Err(e) => {
                error!("Failed to create AsyncDZClient: {e}");
            }
        }
        tokio::time::sleep(std::time::Duration::from_secs(5)).await;
    }
    info!("Websocket task finished successfully");
}
