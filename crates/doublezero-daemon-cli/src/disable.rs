//! `doublezero disable` — stop the reconciler.

use std::io::Write;

use clap::Args;
use doublezero_cli_core::CliContext;

use crate::{client::DaemonClient, ledger::LedgerClient, requirements::check_daemon};

/// Disable the reconciler (tear down tunnels and stop managing them)
#[derive(Args, Debug)]
pub struct Disable {}

impl Disable {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        _ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        check_daemon(daemon, ledger).await?;

        if let Ok(v2) = daemon.v2_status().await {
            if !v2.reconciler_enabled {
                writeln!(out, "Reconciler already disabled")?;
                return Ok(());
            }
            let has_active = v2.services.iter().any(|s| s.status.user_type.is_some());
            if has_active {
                writeln!(out, "Active tunnel(s) will be torn down")?;
            }
        }

        daemon.disable().await?;
        writeln!(out, "Reconciler disabled")?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        client::{
            DoubleZeroStatus, MockDaemonClient, StatusResponse, V2ServiceStatus, V2StatusResponse,
        },
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
    fn test_disable_success() {
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
            daemon.expect_disable().returning(|| Ok(()));

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Disable {}.execute(&ctx, &daemon, &ledger, &mut out).await;

            assert!(result.is_ok());
            let output = String::from_utf8(out).unwrap();
            assert_eq!(output, "Reconciler disabled\n");
        });
    }

    #[test]
    fn test_disable_already_disabled() {
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

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Disable {}.execute(&ctx, &daemon, &ledger, &mut out).await;

            assert!(result.is_ok());
            let output = String::from_utf8(out).unwrap();
            assert_eq!(output, "Reconciler already disabled\n");
        });
    }

    #[test]
    fn test_disable_with_active_tunnels() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let mut ledger = MockLedgerClient::new();
            setup_passing_checks(&mut daemon, &mut ledger);
            daemon.expect_v2_status().returning(|| {
                Ok(V2StatusResponse {
                    reconciler_enabled: true,
                    client_ip: String::new(),
                    network: String::new(),
                    services: vec![V2ServiceStatus {
                        status: StatusResponse {
                            doublezero_status: DoubleZeroStatus {
                                session_status: "BGP Session Up".to_string(),
                                last_session_update: None,
                            },
                            tunnel_name: Some("doublezero1".to_string()),
                            tunnel_src: None,
                            tunnel_dst: None,
                            doublezero_ip: None,
                            user_type: Some("IBRL".to_string()),
                        },
                        current_device: String::new(),
                        lowest_latency_device: String::new(),
                        metro: String::new(),
                        tenant: String::new(),
                        multicast_groups: Default::default(),
                    }],
                })
            });
            daemon.expect_disable().returning(|| Ok(()));

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Disable {}.execute(&ctx, &daemon, &ledger, &mut out).await;

            assert!(result.is_ok());
            let output = String::from_utf8(out).unwrap();
            assert!(output.contains("Active tunnel(s) will be torn down"));
            assert!(output.contains("Reconciler disabled"));
        });
    }

    #[test]
    fn test_disable_daemon_error() {
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
            daemon
                .expect_disable()
                .returning(|| Err(eyre::eyre!("connection refused")));

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Disable {}.execute(&ctx, &daemon, &ledger, &mut out).await;

            assert!(result.is_err());
            assert!(result
                .unwrap_err()
                .to_string()
                .contains("connection refused"));
        });
    }

    #[test]
    fn test_disable_daemon_not_running() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon.expect_daemon_check().return_const(false);
            let mut ledger = MockLedgerClient::new();
            ledger
                .expect_get_environment()
                .returning(Environment::default);

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Disable {}.execute(&ctx, &daemon, &ledger, &mut out).await;

            assert!(result.is_err());
        });
    }
}
