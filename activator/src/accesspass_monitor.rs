use doublezero_sdk::{
    commands::accesspass::{
        check_status::CheckStatusAccessPassCommand, list::ListAccessPassCommand,
    },
    DoubleZeroClient,
};
use std::{
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc,
    },
    time::Duration,
};

pub async fn access_pass_monitor_task<T: DoubleZeroClient>(
    client: Arc<T>,
    stop_signal: Arc<AtomicBool>,
) -> eyre::Result<()> {
    while !stop_signal.load(Ordering::Relaxed) {
        // Monitor users and perform necessary actions

        let epoch = client.get_epoch()?;
        // Read data on-chain
        let accesspass = ListAccessPassCommand.execute(client.as_ref())?;

        for accesspass in accesspass.values() {
            if accesspass.last_access_epoch < epoch {
                CheckStatusAccessPassCommand {
                    client_ip: accesspass.client_ip,
                    user_payer: accesspass.user_payer,
                }
                .execute(client.as_ref())?;
            }
        }

        // Sleep for a while before the next iteration
        tokio::time::sleep(Duration::from_secs(crate::constants::SLEEP_DURATION_SECS)).await;
    }

    Ok(())
}
