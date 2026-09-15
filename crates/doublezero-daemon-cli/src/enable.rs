//! `doublezero enable` — start the reconciler.

use std::{io::Write, net::Ipv4Addr};

use clap::Args;
use doublezero_cli_core::CliContext;

use crate::{client::DaemonClient, ledger::LedgerClient, requirements::check_daemon};

/// Enable the reconciler (start managing tunnels)
#[derive(Args, Debug)]
pub struct Enable {}

/// Turn the reconciler on unless it already is, returning whether it was
/// *already* enabled so the caller can decide what to report.
///
/// `client_ip` is `connect --client-ip`: the address the daemon must provision against
/// instead of the one it discovered. An already-enabled reconciler still has to be told
/// about it — adopting the address is the request, not flipping the flag — so a pin skips
/// the early return that makes an ordinary enable a no-op. Left on its discovered address,
/// the daemon would never match the user the caller is about to create.
///
/// Assumes the caller has already run [`check_daemon`]; `Enable::execute` and
/// the bare `doublezero connect` share this so the two cannot drift.
pub(crate) async fn ensure_reconciler_enabled<D: DaemonClient>(
    daemon: &D,
    client_ip: Option<Ipv4Addr>,
) -> eyre::Result<bool> {
    let already_enabled = daemon
        .v2_status()
        .await
        .map(|v2| v2.reconciler_enabled)
        .unwrap_or(false);

    if already_enabled && client_ip.is_none() {
        return Ok(true);
    }

    daemon.enable(client_ip).await?;
    Ok(already_enabled)
}

impl Enable {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        _ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        check_daemon(daemon, ledger).await?;

        if ensure_reconciler_enabled(daemon, None).await? {
            writeln!(out, "Reconciler already enabled")?;
        } else {
            writeln!(out, "Reconciler enabled")?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        client::{MockDaemonClient, V2StatusResponse},
        ledger::MockLedgerClient,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_config::Environment;
    use std::sync::{Arc, Mutex};

    fn setup_passing_checks(daemon: &mut MockDaemonClient, ledger: &mut MockLedgerClient) {
        daemon.expect_daemon_check().return_const(true);
        daemon.expect_daemon_can_open().return_const(true);
        daemon
            .expect_get_env()
            .returning(|| Ok(Environment::default()));
        ledger
            .expect_get_environment()
            .returning(Environment::default);
    }

    #[test]
    fn test_enable_success() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let mut ledger = MockLedgerClient::new();
            setup_passing_checks(&mut daemon, &mut ledger);
            daemon.expect_v2_status().returning(|| {
                Ok(V2StatusResponse {
                    reconciler_enabled: false,
                    client_ip: String::new(),
                    network: String::new(),
                    services: vec![],
                })
            });
            daemon.expect_enable().returning(|_| Ok(()));

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Enable {}.execute(&ctx, &daemon, &ledger, &mut out).await;

            assert!(result.is_ok());
            let output = String::from_utf8(out).unwrap();
            assert_eq!(output, "Reconciler enabled\n");
        });
    }

    #[test]
    fn test_enable_already_enabled() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let mut ledger = MockLedgerClient::new();
            setup_passing_checks(&mut daemon, &mut ledger);
            daemon.expect_v2_status().returning(|| {
                Ok(V2StatusResponse {
                    reconciler_enabled: true,
                    client_ip: String::new(),
                    network: String::new(),
                    services: vec![],
                })
            });

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Enable {}.execute(&ctx, &daemon, &ledger, &mut out).await;

            assert!(result.is_ok());
            let output = String::from_utf8(out).unwrap();
            assert_eq!(output, "Reconciler already enabled\n");
        });
    }

    /// A pin has to reach an already-enabled daemon. The early return that makes an ordinary
    /// enable a no-op would leave it provisioning the address it discovered, which is not the
    /// address the caller is about to create a user for.
    #[test]
    fn test_ensure_reconciler_enabled_delivers_a_pin_when_already_enabled() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon.expect_v2_status().returning(|| {
                Ok(V2StatusResponse {
                    reconciler_enabled: true,
                    client_ip: "1.2.3.4".to_string(),
                    network: String::new(),
                    services: vec![],
                })
            });
            let seen = Arc::new(Mutex::new(None));
            let sink = seen.clone();
            daemon.expect_enable().times(1).returning(move |ip| {
                *sink.lock().unwrap() = Some(ip);
                Ok(())
            });

            let pinned = Ipv4Addr::new(203, 0, 113, 9);
            let already_enabled = ensure_reconciler_enabled(&daemon, Some(pinned))
                .await
                .expect("the pin was accepted");

            assert!(
                already_enabled,
                "the reconciler was already on; delivering a pin does not change that"
            );
            assert_eq!(*seen.lock().unwrap(), Some(Some(pinned)));
        });
    }

    /// Without a pin an already-enabled daemon is left alone — the no-op every caller that
    /// does not pass the flag depends on.
    #[test]
    fn test_ensure_reconciler_enabled_sends_nothing_when_already_enabled() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon.expect_v2_status().returning(|| {
                Ok(V2StatusResponse {
                    reconciler_enabled: true,
                    client_ip: "1.2.3.4".to_string(),
                    network: String::new(),
                    services: vec![],
                })
            });
            daemon.expect_enable().never();

            assert!(ensure_reconciler_enabled(&daemon, None).await.unwrap());
        });
    }

    #[test]
    fn test_enable_daemon_error() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let mut ledger = MockLedgerClient::new();
            setup_passing_checks(&mut daemon, &mut ledger);
            daemon.expect_v2_status().returning(|| {
                Ok(V2StatusResponse {
                    reconciler_enabled: false,
                    client_ip: String::new(),
                    network: String::new(),
                    services: vec![],
                })
            });
            daemon
                .expect_enable()
                .returning(|_| Err(eyre::eyre!("connection refused")));

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Enable {}.execute(&ctx, &daemon, &ledger, &mut out).await;

            assert!(result.is_err());
            assert!(result
                .unwrap_err()
                .to_string()
                .contains("connection refused"));
        });
    }

    #[test]
    fn test_enable_daemon_not_running() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon.expect_daemon_check().return_const(false);
            let mut ledger = MockLedgerClient::new();
            ledger
                .expect_get_environment()
                .returning(Environment::default);

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Enable {}.execute(&ctx, &daemon, &ledger, &mut out).await;

            assert!(result.is_err());
        });
    }
}
