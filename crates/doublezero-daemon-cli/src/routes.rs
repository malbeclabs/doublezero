//! `doublezero routes` — show installed routes, plus the route-retrieval
//! utility.

use std::io::Write;

use clap::Args;
use doublezero_cli_core::CliContext;
use indicatif::ProgressBar;

use crate::{
    client::{DaemonClient, RouteRecord},
    helpers,
    ledger::LedgerClient,
    requirements::check_daemon,
};

/// Fetch installed routes from the daemon, erroring when none are present.
pub async fn retrieve_routes<D: DaemonClient>(
    daemon: &D,
    spinner: Option<&ProgressBar>,
) -> eyre::Result<Vec<RouteRecord>> {
    if let Some(spinner) = spinner {
        spinner.set_message("Retrieving routes...");
    }

    let routes = daemon.routes().await.map_err(|e| eyre::eyre!(e))?;
    if routes.is_empty() {
        return Err(eyre::eyre!("No routes found"));
    }
    Ok(routes)
}

/// View your installed routes
#[derive(Args, Debug)]
pub struct Routes {
    /// Output as json
    #[arg(long, default_value = "false")]
    json: bool,
}

impl Routes {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        _ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        check_daemon(daemon, ledger).await?;

        let routes = retrieve_routes(daemon, None).await?;
        helpers::show_output(routes, self.json, out)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{client::MockDaemonClient, ledger::MockLedgerClient};
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_config::Environment;

    fn make_route(local_ip: &str, peer_ip: &str) -> RouteRecord {
        RouteRecord {
            network: "test".to_string(),
            local_ip: local_ip.to_string(),
            kernel_state: "present".to_string(),
            liveness_last_updated: None,
            liveness_state: None,
            liveness_state_reason: None,
            peer_ip: peer_ip.to_string(),
            peer_client_version: None,
        }
    }

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
    fn test_retrieve_routes() {
        block_on(async {
            let routes = vec![
                make_route("192.168.1.1", "192.168.1.2"),
                make_route("192.168.1.2", "192.168.1.3"),
                make_route("192.168.1.3", "192.168.1.4"),
            ];

            let mut daemon = MockDaemonClient::new();
            let expected_routes = routes.clone();
            daemon
                .expect_routes()
                .returning(move || Ok(expected_routes.clone()));

            let result = retrieve_routes(&daemon, None).await.unwrap();
            assert_eq!(result.len(), 3);
            assert_eq!(result, routes);
        });
    }

    #[test]
    fn test_retrieve_routes_empty_errors() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon.expect_routes().returning(|| Ok(vec![]));

            let err = retrieve_routes(&daemon, None).await.unwrap_err();
            assert!(err.to_string().contains("No routes found"));
        });
    }

    #[test]
    fn test_routes_verb_json_output() {
        block_on(async {
            let routes = vec![make_route("192.168.1.1", "192.168.1.2")];

            let mut daemon = MockDaemonClient::new();
            let mut ledger = MockLedgerClient::new();
            setup_passing_checks(&mut daemon, &mut ledger);
            let expected_routes = routes.clone();
            daemon
                .expect_routes()
                .returning(move || Ok(expected_routes.clone()));

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Routes { json: true }
                .execute(&ctx, &daemon, &ledger, &mut out)
                .await;

            assert!(result.is_ok());
            let output = String::from_utf8(out).unwrap();
            let parsed: Vec<RouteRecord> = serde_json::from_str(output.trim()).unwrap();
            assert_eq!(parsed, routes);
        });
    }

    #[test]
    fn test_routes_verb_daemon_not_running() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon.expect_daemon_check().return_const(false);
            let mut ledger = MockLedgerClient::new();
            ledger
                .expect_get_environment()
                .returning(Environment::default);

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Routes { json: false }
                .execute(&ctx, &daemon, &ledger, &mut out)
                .await;

            assert!(result.is_err());
        });
    }
}
