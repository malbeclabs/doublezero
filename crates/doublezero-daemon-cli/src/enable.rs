//! `doublezero enable` — start the reconciler.

use std::io::Write;

use clap::Args;
use doublezero_cli_core::CliContext;

use crate::{client::DaemonClient, ledger::LedgerClient, requirements::check_daemon};

/// Enable the reconciler (start managing tunnels)
#[derive(Args, Debug)]
pub struct Enable {}

impl Enable {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        _ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        check_daemon(daemon, ledger).await?;

        if let Ok(v2) = daemon.v2_status().await {
            if v2.reconciler_enabled {
                writeln!(out, "Reconciler already enabled")?;
                return Ok(());
            }
        }

        daemon.enable().await?;
        writeln!(out, "Reconciler enabled")?;
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
            daemon.expect_enable().returning(|| Ok(()));

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
                .returning(|| Err(eyre::eyre!("connection refused")));

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
