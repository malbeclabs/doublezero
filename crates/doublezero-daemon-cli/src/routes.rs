//! `doublezero routes` — show installed routes, plus the route-retrieval
//! utility. `doublezero routes status` summarizes them.

use std::io::Write;

use clap::{Args, Subcommand};
use doublezero_cli_core::CliContext;
use indicatif::ProgressBar;
use tabled::{settings::Style, Table};

use crate::{
    client::{route_action_problem, DaemonClient, RouteRecord, DOCS_MISSING_PEER_ROUTES},
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
    #[command(subcommand)]
    command: Option<RoutesCommand>,

    /// Output as json
    #[arg(long, default_value = "false", global = true)]
    json: bool,
}

#[derive(Subcommand, Debug)]
enum RoutesCommand {
    /// Summarize your peer routes and say whether you must act
    Status,
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

        match self.command {
            None => {
                let routes = retrieve_routes(daemon, None).await?;
                helpers::show_output(routes, self.json, out)?;
            }
            Some(RoutesCommand::Status) => show_route_summary(daemon, self.json, out).await?,
        }

        Ok(())
    }
}

async fn show_route_summary<D: DaemonClient, W: Write>(
    daemon: &D,
    json: bool,
    out: &mut W,
) -> eyre::Result<()> {
    let v2_status = daemon.v2_status().await?;
    let ibrl = v2_status
        .services
        .iter()
        .find(|svc| {
            matches!(
                svc.status.user_type.as_deref(),
                Some("IBRL" | "IBRLWithAllocatedIP")
            )
        })
        .ok_or_else(|| eyre::eyre!("No IBRL service found"))?;
    let summary = ibrl.routes.as_ref().ok_or_else(|| {
        eyre::eyre!(
            "doublezerod reports no route summary: route liveness is off, doublezerod is older \
             than this CLI, or it could not read the kernel routes (see its log)"
        )
    })?;

    if json {
        writeln!(out, "{}", serde_json::to_string_pretty(summary)?)?;
        return Ok(());
    }
    let table = Table::new([summary])
        .with(Style::psql().remove_horizontals())
        .to_string();
    writeln!(out, "{table}")?;
    let missing: u64 = summary.not_installed.iter().map(|g| g.count).sum();
    if let Some(problem) = route_action_problem(&summary.action) {
        writeln!(
            out,
            "\n⚠️  Missing peer routes: {problem}. Check that {} UDP 44880 is allowed on doublezero0.",
            summary.action
        )?;
    } else if summary.action == "none" && missing == 1 {
        writeln!(out, "\nThe route to 1 peer is not installed, so traffic to it uses the public internet. This host appears healthy and needs no action.")?;
    } else if summary.action == "none" && missing > 1 {
        writeln!(out, "\nRoutes to {missing} peers are not installed, so traffic to them uses the public internet. This host appears healthy and needs no action.")?;
    } else {
        return Ok(());
    }
    writeln!(out, "See {DOCS_MISSING_PEER_ROUTES}")?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        client::{
            DoubleZeroStatus, MockDaemonClient, RouteGroup, RouteSummary, StatusResponse,
            V2ServiceStatus, V2StatusResponse,
        },
        ledger::MockLedgerClient,
    };
    use clap::Parser;
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
            let result = Routes {
                command: None,
                json: true,
            }
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
            let result = Routes {
                command: None,
                json: false,
            }
            .execute(&ctx, &daemon, &ledger, &mut out)
            .await;

            assert!(result.is_err());
        });
    }

    #[derive(Parser)]
    struct TestCli {
        #[command(subcommand)]
        command: TestCommand,
    }

    #[derive(clap::Subcommand)]
    enum TestCommand {
        Routes(Routes),
    }

    fn parse(args: &[&str]) -> Routes {
        let TestCommand::Routes(routes) = TestCli::try_parse_from(args).unwrap().command;
        routes
    }

    #[test]
    fn test_routes_status_is_an_optional_subcommand() {
        let routes = parse(&["doublezero", "routes"]);
        assert!(routes.command.is_none() && !routes.json);
        let routes = parse(&["doublezero", "routes", "--json"]);
        assert!(routes.command.is_none() && routes.json);
        let routes = parse(&["doublezero", "routes", "status"]);
        assert!(matches!(routes.command, Some(RoutesCommand::Status)) && !routes.json);
        let routes = parse(&["doublezero", "routes", "status", "--json"]);
        assert!(matches!(routes.command, Some(RoutesCommand::Status)) && routes.json);
    }

    const DOCS: &str =
        "https://docs.doublezero.xyz/support/troubleshooting#issue-missing-peer-routes";

    fn summary(action: &str, installed: u64, not_installed: Vec<RouteGroup>) -> RouteSummary {
        RouteSummary {
            peers: installed + not_installed.iter().map(|g| g.count).sum::<u64>(),
            installed,
            not_installed,
            action: action.to_string(),
            docs: (action != "none").then(|| DOCS.to_string()),
        }
    }

    fn group(state: &str, reason: &str, count: u64) -> RouteGroup {
        RouteGroup {
            state: state.to_string(),
            reason: reason.to_string(),
            count,
        }
    }

    fn v2_status(user_type: &str, routes: Option<RouteSummary>) -> V2StatusResponse {
        V2StatusResponse {
            reconciler_enabled: true,
            client_ip: "1.2.3.4".to_string(),
            network: "testnet".to_string(),
            services: vec![V2ServiceStatus {
                status: StatusResponse {
                    doublezero_status: DoubleZeroStatus {
                        session_status: "BGP Session Up".to_string(),
                        last_session_update: None,
                    },
                    tunnel_name: Some("doublezero0".to_string()),
                    tunnel_src: None,
                    tunnel_dst: None,
                    doublezero_ip: Some("1.2.3.4".to_string()),
                    user_type: Some(user_type.to_string()),
                },
                current_device: String::new(),
                lowest_latency_device: String::new(),
                metro: String::new(),
                tenant: String::new(),
                multicast_groups: Default::default(),
                subscriptions: Vec::new(),
                routes,
            }],
        }
    }

    fn run_status(response: V2StatusResponse, json: bool) -> eyre::Result<String> {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let mut ledger = MockLedgerClient::new();
            setup_passing_checks(&mut daemon, &mut ledger);
            daemon
                .expect_v2_status()
                .returning(move || Ok(response.clone()));

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            Routes {
                command: Some(RoutesCommand::Status),
                json,
            }
            .execute(&ctx, &daemon, &ledger, &mut out)
            .await?;
            Ok(String::from_utf8(out).unwrap())
        })
    }

    #[test]
    fn test_routes_status_table() {
        let none = summary("none", 9, vec![group("down", "no reply", 3)]);
        let output = run_status(v2_status("IBRL", Some(none)), false).unwrap();
        assert_eq!(
            output,
            format!(
                concat!(
                    " Peers | Installed | Not Installed      | Action \n",
                    " 12    | 9         | down (no reply): 3 | none   \n",
                    "\n",
                    "Routes to 3 peers are not installed, so traffic to them uses the public internet. ",
                    "This host appears healthy and needs no action.\n",
                    "See {}\n",
                ),
                DOCS
            )
        );

        let one = summary("none", 4, vec![group("up", "held down", 1)]);
        let output = run_status(v2_status("IBRL", Some(one)), false).unwrap();
        assert_eq!(
            output,
            format!(
                concat!(
                    " Peers | Installed | Not Installed     | Action \n",
                    " 5     | 4         | up (held down): 1 | none   \n",
                    "\n",
                    "The route to 1 peer is not installed, so traffic to it uses the public internet. ",
                    "This host appears healthy and needs no action.\n",
                    "See {}\n",
                ),
                DOCS
            )
        );

        let all = summary("none", 5, vec![]);
        let output = run_status(v2_status("IBRL", Some(all)), false).unwrap();
        assert_eq!(
            output,
            concat!(
                " Peers | Installed | Not Installed | Action \n",
                " 5     | 5         |               | none   \n",
            )
        );

        let inbound = summary("inbound", 0, vec![group("down", "no reply", 12)]);
        let output = run_status(v2_status("IBRL", Some(inbound)), false).unwrap();
        assert_eq!(
            output,
            format!(
                concat!(
                    " Peers | Installed | Not Installed       | Action  \n",
                    " 12    | 0         | down (no reply): 12 | inbound \n",
                    "\n",
                    "⚠️  Missing peer routes: this host receives no liveness probes from peers. ",
                    "Check that inbound UDP 44880 is allowed on doublezero0.\n",
                    "See {}\n",
                ),
                DOCS
            )
        );

        let outbound = summary(
            "outbound",
            0,
            vec![group("down", "timeout", 2), group("init", "", 1)],
        );
        let output = run_status(v2_status("IBRLWithAllocatedIP", Some(outbound)), false).unwrap();
        assert_eq!(
            output,
            format!(
                concat!(
                    " Peers | Installed | Not Installed              | Action   \n",
                    " 3     | 0         | down (timeout): 2, init: 1 | outbound \n",
                    "\n",
                    "⚠️  Missing peer routes: peers receive no liveness probes from this host. ",
                    "Check that outbound UDP 44880 is allowed on doublezero0.\n",
                    "See {}\n",
                ),
                DOCS
            )
        );
    }

    #[test]
    fn test_routes_status_json() {
        let inbound = summary("inbound", 0, vec![group("down", "no reply", 2)]);
        let output = run_status(v2_status("IBRL", Some(inbound)), true).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(output.trim()).unwrap();
        assert_eq!(
            parsed,
            serde_json::json!({
                "peers": 2,
                "installed": 0,
                "not_installed": [{"state": "down", "reason": "no reply", "count": 2}],
                "action": "inbound",
                "docs": DOCS,
            })
        );

        let none = summary("none", 1, vec![]);
        let output = run_status(v2_status("IBRL", Some(none)), true).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(output.trim()).unwrap();
        assert_eq!(parsed["action"], "none");
        assert!(parsed.get("docs").is_none());
    }

    #[test]
    fn test_routes_status_without_a_summary() {
        let err = run_status(v2_status("IBRL", None), false).unwrap_err();
        assert_eq!(
            err.to_string(),
            "doublezerod reports no route summary: route liveness is off, doublezerod is older than this CLI, or it could not read the kernel routes (see its log)"
        );

        let err = run_status(v2_status("Multicast", None), false).unwrap_err();
        assert_eq!(err.to_string(), "No IBRL service found");
    }
}
