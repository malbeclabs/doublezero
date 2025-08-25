use doublezero_sdk::{
    commands::accesspass::{
        check_status::CheckStatusAccessPassCommand, list::ListAccessPassCommand,
    },
    DZClient,
};
use log::info;
use std::{
    path::PathBuf,
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc,
    },
    thread,
    time::Duration,
};

pub fn process_access_pass_monitor_thread(
    rpc_url: String,
    websocket_url: String,
    program_id: String,
    keypair: PathBuf,
    stop_signal: Arc<AtomicBool>,
) -> eyre::Result<()> {
    info!("User monitor thread started");

    let client = DZClient::new(
        Some(rpc_url.clone()),
        Some(websocket_url.clone()),
        Some(program_id.clone()),
        Some(keypair.clone()),
    )?;

    while !stop_signal.load(Ordering::Relaxed) {
        // Monitor users and perform necessary actions

        let epoch = client.get_epoch()?;
        // Read data on-chain
        let accesspass = ListAccessPassCommand.execute(&client)?;

        for accesspass in accesspass.values() {
            if accesspass.last_access_epoch < epoch {
                CheckStatusAccessPassCommand {
                    client_ip: accesspass.client_ip,
                    user_payer: accesspass.user_payer,
                }
                .execute(&client)?;
            }
        }

        // Sleep for a while before the next iteration
        thread::sleep(Duration::from_secs(crate::constants::SLEEP_DURATION_SECS));
    }

    Ok(())
}
