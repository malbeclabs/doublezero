//! Shared pre-flight check for daemon verbs.
//!
//! Validates that the daemon socket is present and accessible, and that the
//! daemon and ledger agree on the active environment.

use crate::{client::DaemonClient, ledger::LedgerClient};

pub(crate) async fn check_daemon<D: DaemonClient, L: LedgerClient>(
    daemon: &D,
    ledger: &L,
) -> eyre::Result<()> {
    if !daemon.daemon_check() {
        tracing::warn!("doublezero service is not accessible.");
        eyre::bail!("Please start the doublezerod service.");
    }

    if !daemon.daemon_can_open() {
        tracing::warn!("doublezero service is not accessible.");
        eyre::bail!("Please check the permissions of the doublezerod service.");
    }

    let daemon_env = daemon.get_env().await?;
    let client_env = ledger.get_environment();
    if daemon_env != client_env {
        return Err(eyre::eyre!(
            "The client and the daemon are using different environments.\n\
Client: {}\n\
Daemon: {}\n\
Please update the daemon configuration so both use the same environment.",
            client_env,
            daemon_env
        ));
    }

    Ok(())
}
