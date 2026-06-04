//! `doublezero status` — show daemon service status.

use std::io::Write;

use backon::{ExponentialBuilder, Retryable};
use clap::Args;
use doublezero_cli_core::CliContext;
use serde::{Deserialize, Serialize};
use std::time::Duration;
use tabled::Tabled;

use crate::{
    client::{DaemonClient, DoubleZeroStatus, MulticastGroups, StatusResponse, Subscription},
    helpers,
    ledger::LedgerClient,
    requirements::check_daemon,
};

/// Get the status of your service
#[derive(Args, Debug)]
pub struct Status {
    /// Output as json
    #[arg(long, default_value = "false")]
    json: bool,
}

#[derive(Tabled, Debug, Deserialize, Serialize)]
struct AppendedStatusResponse {
    #[tabled(inline)]
    response: StatusResponse,
    #[tabled(rename = "Reconciler")]
    reconciler_enabled: bool,
    #[tabled(rename = "Tenant")]
    tenant: String,
    #[tabled(rename = "Current Device")]
    current_device: String,
    #[tabled(rename = "Lowest Latency Device")]
    lowest_latency_device: String,
    #[tabled(rename = "Metro")]
    metro: String,
    #[tabled(rename = "Network")]
    network: String,
    #[tabled(rename = "Multicast Groups")]
    multicast_groups: String,
    #[tabled(skip)]
    subscriptions: Vec<Subscription>,
}

fn format_multicast_groups(groups: &MulticastGroups) -> String {
    let mut parts = Vec::new();
    for code in &groups.publisher {
        parts.push(format!("P:{code}"));
    }
    for code in &groups.subscriber {
        parts.push(format!("S:{code}"));
    }
    parts.join(",")
}

impl Status {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        _ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        check_daemon(daemon, ledger).await?;
        let responses = self.build_status(daemon, ledger).await?;
        helpers::show_output(responses, self.json, out)?;
        Ok(())
    }

    async fn build_status<D: DaemonClient, L: LedgerClient>(
        &self,
        daemon: &D,
        ledger: &L,
    ) -> eyre::Result<Vec<AppendedStatusResponse>> {
        let backoff = ExponentialBuilder::new()
            .with_max_times(3)
            .with_min_delay(Duration::from_millis(500))
            .with_max_delay(Duration::from_secs(2));
        let v2_status = (|| daemon.v2_status()).retry(backoff).await?;

        // When no services are running, synthesize a "disconnected" entry to match
        // the legacy /status endpoint behavior. The QA agent and other tooling
        // expect at least one entry in the status array.
        if v2_status.services.is_empty() {
            return Ok(vec![AppendedStatusResponse {
                response: StatusResponse {
                    doublezero_status: DoubleZeroStatus {
                        session_status: "disconnected".to_string(),
                        last_session_update: None,
                    },
                    tunnel_name: None,
                    tunnel_src: None,
                    tunnel_dst: None,
                    doublezero_ip: None,
                    user_type: None,
                },
                reconciler_enabled: v2_status.reconciler_enabled,
                tenant: String::new(),
                current_device: "N/A".to_string(),
                lowest_latency_device: "N/A".to_string(),
                metro: "N/A".to_string(),
                network: if v2_status.network.is_empty() {
                    format!("{}", ledger.get_environment())
                } else {
                    v2_status.network.clone()
                },
                multicast_groups: String::new(),
                subscriptions: Vec::new(),
            }]);
        }

        let network = if v2_status.network.is_empty() {
            format!("{}", ledger.get_environment())
        } else {
            v2_status.network.clone()
        };

        let mut responses = Vec::with_capacity(v2_status.services.len());
        for svc in &v2_status.services {
            let current_device = if svc.current_device.is_empty() {
                "N/A".to_string()
            } else {
                svc.current_device.clone()
            };
            let metro = if svc.metro.is_empty() {
                "N/A".to_string()
            } else {
                svc.metro.clone()
            };

            // Apply display formatting for lowest_latency_device.
            let lowest_latency_device = if svc.lowest_latency_device.is_empty() {
                "N/A".to_string()
            } else if self.json || svc.status.doublezero_status.session_status != "BGP Session Up" {
                svc.lowest_latency_device.clone()
            } else if svc.lowest_latency_device == current_device {
                format!("✅ {}", svc.lowest_latency_device)
            } else if current_device != "N/A" {
                format!("⚠️ {}", svc.lowest_latency_device)
            } else {
                svc.lowest_latency_device.clone()
            };

            responses.push(AppendedStatusResponse {
                response: svc.status.clone(),
                reconciler_enabled: v2_status.reconciler_enabled,
                current_device,
                lowest_latency_device,
                metro,
                network: network.clone(),
                tenant: svc.tenant.clone(),
                multicast_groups: format_multicast_groups(&svc.multicast_groups),
                subscriptions: svc.subscriptions.clone(),
            });
        }

        Ok(responses)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        client::{MockDaemonClient, V2ServiceStatus, V2StatusResponse},
        ledger::MockLedgerClient,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_config::Environment;
    use std::sync::{
        atomic::{AtomicUsize, Ordering},
        Arc,
    };

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

    #[allow(clippy::too_many_arguments)]
    fn make_v2_service(
        session_status: &str,
        tunnel_name: Option<&str>,
        tunnel_src: Option<&str>,
        tunnel_dst: Option<&str>,
        doublezero_ip: Option<&str>,
        user_type: Option<&str>,
        current_device: &str,
        lowest_latency_device: &str,
        metro: &str,
        tenant: &str,
    ) -> V2ServiceStatus {
        V2ServiceStatus {
            status: StatusResponse {
                doublezero_status: DoubleZeroStatus {
                    session_status: session_status.to_string(),
                    last_session_update: Some(1_625_247_600),
                },
                tunnel_name: tunnel_name.map(String::from),
                tunnel_src: tunnel_src.map(String::from),
                tunnel_dst: tunnel_dst.map(String::from),
                doublezero_ip: doublezero_ip.map(String::from),
                user_type: user_type.map(String::from),
            },
            current_device: current_device.to_string(),
            lowest_latency_device: lowest_latency_device.to_string(),
            metro: metro.to_string(),
            tenant: tenant.to_string(),
            multicast_groups: MulticastGroups::default(),
            subscriptions: Vec::new(),
        }
    }

    fn make_status_response(daemon: &mut MockDaemonClient, response: V2StatusResponse) {
        daemon
            .expect_v2_status()
            .returning(move || Ok(response.clone()));
    }

    #[test]
    fn test_status_tunnel_up() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let mut ledger = MockLedgerClient::new();
            setup_passing_checks(&mut daemon, &mut ledger);
            make_status_response(
                &mut daemon,
                V2StatusResponse {
                    reconciler_enabled: true,
                    client_ip: String::new(),
                    network: "testnet".to_string(),
                    services: vec![make_v2_service(
                        "BGP Session Up",
                        Some("tunnel_name"),
                        Some("1.2.3.4"),
                        Some("42.42.42.42"),
                        Some("1.2.3.4"),
                        Some("IBRL"),
                        "device1",
                        "device2",
                        "metro",
                        "",
                    )],
                },
            );

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Status { json: true }
                .execute(&ctx, &daemon, &ledger, &mut out)
                .await;

            assert!(result.is_ok());
            let output = String::from_utf8(out).unwrap();
            let parsed: Vec<AppendedStatusResponse> = serde_json::from_str(output.trim()).unwrap();
            assert_eq!(parsed.len(), 1);
            assert_eq!(
                parsed[0].response.doublezero_status.session_status,
                "BGP Session Up"
            );
            assert_eq!(
                parsed[0].response.tunnel_name.as_deref(),
                Some("tunnel_name")
            );
            assert_eq!(parsed[0].response.tunnel_src.as_deref(), Some("1.2.3.4"));
            assert_eq!(
                parsed[0].response.tunnel_dst.as_deref(),
                Some("42.42.42.42")
            );
            assert_eq!(parsed[0].response.doublezero_ip.as_deref(), Some("1.2.3.4"));
            assert_eq!(parsed[0].response.user_type.as_deref(), Some("IBRL"));
            assert_eq!(parsed[0].current_device, "device1");
            assert_eq!(parsed[0].lowest_latency_device, "device2");
            assert_eq!(parsed[0].metro, "metro");
            assert_eq!(parsed[0].network, "testnet");
        });
    }

    #[test]
    fn test_status_tunnel_down() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let mut ledger = MockLedgerClient::new();
            setup_passing_checks(&mut daemon, &mut ledger);
            make_status_response(
                &mut daemon,
                V2StatusResponse {
                    reconciler_enabled: true,
                    client_ip: String::new(),
                    network: "testnet".to_string(),
                    services: vec![V2ServiceStatus {
                        status: StatusResponse {
                            doublezero_status: DoubleZeroStatus {
                                session_status: "BGP Session Down".to_string(),
                                last_session_update: None,
                            },
                            tunnel_name: None,
                            tunnel_src: None,
                            tunnel_dst: None,
                            doublezero_ip: None,
                            user_type: None,
                        },
                        current_device: String::new(),
                        lowest_latency_device: "device2".to_string(),
                        metro: String::new(),
                        tenant: String::new(),
                        multicast_groups: MulticastGroups::default(),
                        subscriptions: Vec::new(),
                    }],
                },
            );

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Status { json: true }
                .execute(&ctx, &daemon, &ledger, &mut out)
                .await;

            assert!(result.is_ok());
            let output = String::from_utf8(out).unwrap();
            let parsed: Vec<AppendedStatusResponse> = serde_json::from_str(output.trim()).unwrap();
            assert_eq!(parsed.len(), 1);
            assert_eq!(
                parsed[0].response.doublezero_status.session_status,
                "BGP Session Down"
            );
            assert_eq!(parsed[0].current_device, "N/A");
            assert_eq!(parsed[0].lowest_latency_device, "device2");
            assert_eq!(parsed[0].metro, "N/A");
            assert_eq!(parsed[0].network, "testnet");
        });
    }

    #[test]
    fn test_status_enriched_from_daemon() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let mut ledger = MockLedgerClient::new();
            setup_passing_checks(&mut daemon, &mut ledger);
            make_status_response(
                &mut daemon,
                V2StatusResponse {
                    reconciler_enabled: true,
                    client_ip: String::new(),
                    network: "testnet".to_string(),
                    services: vec![make_v2_service(
                        "BGP Session Up",
                        Some("tunnel_name"),
                        Some("20.20.20.20"),
                        Some("42.42.42.42"),
                        Some("1.2.3.4"),
                        Some("IBRL"),
                        "device1",
                        "device1",
                        "metro",
                        "",
                    )],
                },
            );

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Status { json: true }
                .execute(&ctx, &daemon, &ledger, &mut out)
                .await;

            assert!(result.is_ok());
            let output = String::from_utf8(out).unwrap();
            let parsed: Vec<AppendedStatusResponse> = serde_json::from_str(output.trim()).unwrap();
            assert_eq!(parsed[0].current_device, "device1");
            assert_eq!(parsed[0].metro, "metro");
        });
    }

    #[test]
    fn test_status_reconciler_disabled() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let mut ledger = MockLedgerClient::new();
            setup_passing_checks(&mut daemon, &mut ledger);
            make_status_response(
                &mut daemon,
                V2StatusResponse {
                    reconciler_enabled: false,
                    client_ip: String::new(),
                    network: "testnet".to_string(),
                    services: vec![make_v2_service(
                        "BGP Session Up",
                        Some("doublezero1"),
                        Some("1.2.3.4"),
                        Some("5.6.7.8"),
                        Some("10.0.0.1"),
                        Some("IBRL"),
                        "",
                        "",
                        "",
                        "",
                    )],
                },
            );

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Status { json: true }
                .execute(&ctx, &daemon, &ledger, &mut out)
                .await;

            assert!(result.is_ok());
            let output = String::from_utf8(out).unwrap();
            let parsed: Vec<AppendedStatusResponse> = serde_json::from_str(output.trim()).unwrap();
            assert!(!parsed[0].reconciler_enabled);
        });
    }

    #[test]
    fn test_status_empty_services_disconnected() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let mut ledger = MockLedgerClient::new();
            setup_passing_checks(&mut daemon, &mut ledger);
            make_status_response(
                &mut daemon,
                V2StatusResponse {
                    reconciler_enabled: false,
                    client_ip: String::new(),
                    network: "testnet".to_string(),
                    services: vec![],
                },
            );

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Status { json: true }
                .execute(&ctx, &daemon, &ledger, &mut out)
                .await;

            assert!(result.is_ok());
            let output = String::from_utf8(out).unwrap();
            let parsed: Vec<AppendedStatusResponse> = serde_json::from_str(output.trim()).unwrap();
            assert_eq!(parsed.len(), 1);
            assert_eq!(
                parsed[0].response.doublezero_status.session_status,
                "disconnected"
            );
            assert!(!parsed[0].reconciler_enabled);
            assert_eq!(parsed[0].current_device, "N/A");
            assert_eq!(parsed[0].network, "testnet");
        });
    }

    #[test]
    fn test_status_multicast_groups_display() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let mut ledger = MockLedgerClient::new();
            setup_passing_checks(&mut daemon, &mut ledger);
            daemon.expect_v2_status().returning(|| {
                Ok(V2StatusResponse {
                    reconciler_enabled: true,
                    client_ip: String::new(),
                    network: "testnet".to_string(),
                    services: vec![V2ServiceStatus {
                        status: StatusResponse {
                            doublezero_status: DoubleZeroStatus {
                                session_status: "BGP Session Up".to_string(),
                                last_session_update: Some(1_625_247_600),
                            },
                            tunnel_name: Some("doublezero1".to_string()),
                            tunnel_src: Some("10.10.10.10".to_string()),
                            tunnel_dst: Some("5.6.7.8".to_string()),
                            doublezero_ip: None,
                            user_type: Some("Multicast".to_string()),
                        },
                        current_device: "device1".to_string(),
                        lowest_latency_device: "device1".to_string(),
                        metro: "metro".to_string(),
                        tenant: String::new(),
                        multicast_groups: MulticastGroups {
                            publisher: vec!["solana-lv".to_string()],
                            subscriber: vec!["solana-ams".to_string()],
                        },
                        subscriptions: vec![
                            Subscription {
                                pubkey: "pubLV".to_string(),
                                code: "solana-lv".to_string(),
                                multicast_ip: "233.84.178.1".to_string(),
                                max_bandwidth: 1_000_000_000,
                                publisher: true,
                                subscriber: false,
                            },
                            Subscription {
                                pubkey: "pubAMS".to_string(),
                                code: "solana-ams".to_string(),
                                multicast_ip: "233.84.178.2".to_string(),
                                max_bandwidth: 2_000_000_000,
                                publisher: false,
                                subscriber: true,
                            },
                        ],
                    }],
                })
            });

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Status { json: true }
                .execute(&ctx, &daemon, &ledger, &mut out)
                .await;

            assert!(result.is_ok());
            let output = String::from_utf8(out).unwrap();
            let parsed: Vec<AppendedStatusResponse> = serde_json::from_str(output.trim()).unwrap();
            assert_eq!(parsed[0].multicast_groups, "P:solana-lv,S:solana-ams");

            // The structured subscriptions list is carried through verbatim.
            assert_eq!(
                parsed[0].subscriptions,
                vec![
                    Subscription {
                        pubkey: "pubLV".to_string(),
                        code: "solana-lv".to_string(),
                        multicast_ip: "233.84.178.1".to_string(),
                        max_bandwidth: 1_000_000_000,
                        publisher: true,
                        subscriber: false,
                    },
                    Subscription {
                        pubkey: "pubAMS".to_string(),
                        code: "solana-ams".to_string(),
                        multicast_ip: "233.84.178.2".to_string(),
                        max_bandwidth: 2_000_000_000,
                        publisher: false,
                        subscriber: true,
                    },
                ]
            );
        });
    }

    /// Test JSON output format contract — validates the exact field names and nesting.
    #[test]
    fn test_status_json_output_format() {
        let status_response = StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "BGP Session Up".to_string(),
                last_session_update: Some(1_625_247_600),
            },
            tunnel_name: Some("doublezero1".to_string()),
            tunnel_src: Some("10.0.0.1".to_string()),
            tunnel_dst: Some("5.6.7.8".to_string()),
            doublezero_ip: Some("10.1.2.3".to_string()),
            user_type: Some("IBRL".to_string()),
        };

        let appended_response = AppendedStatusResponse {
            response: status_response,
            reconciler_enabled: true,
            current_device: "device1".to_string(),
            lowest_latency_device: "device1".to_string(),
            metro: "amsterdam".to_string(),
            network: "Testnet".to_string(),
            tenant: "".to_string(),
            multicast_groups: String::new(),
            subscriptions: vec![Subscription {
                pubkey: "pubLV".to_string(),
                code: "solana-lv".to_string(),
                multicast_ip: "233.84.178.1".to_string(),
                max_bandwidth: 1_000_000_000,
                publisher: true,
                subscriber: false,
            }],
        };

        let json_response = vec![appended_response];
        let json_output = serde_json::to_value(&json_response).expect("Failed to serialize");

        assert!(json_output.is_array());
        assert_eq!(json_output.as_array().unwrap().len(), 1);

        let status = &json_output.as_array().unwrap()[0];
        assert!(status.get("response").is_some());
        assert!(status.get("reconciler_enabled").is_some());
        assert!(status.get("current_device").is_some());
        assert!(status.get("lowest_latency_device").is_some());
        assert!(status.get("metro").is_some());
        assert!(status.get("network").is_some());
        assert!(status.get("tenant").is_some());
        assert!(status.get("multicast_groups").is_some());
        let subscriptions = status
            .get("subscriptions")
            .expect("Missing 'subscriptions' field");
        assert!(subscriptions.is_array(), "subscriptions should be an array");
        let sub = &subscriptions.as_array().unwrap()[0];
        assert_eq!(sub.get("pubkey").unwrap(), "pubLV");
        assert_eq!(sub.get("code").unwrap(), "solana-lv");
        assert_eq!(sub.get("multicast_ip").unwrap(), "233.84.178.1");
        assert_eq!(sub.get("max_bandwidth").unwrap(), 1_000_000_000);
        assert_eq!(sub.get("publisher").unwrap(), true);
        assert_eq!(sub.get("subscriber").unwrap(), false);

        let response = status.get("response").unwrap();
        assert!(response.get("doublezero_status").is_some());
        assert!(response.get("tunnel_name").is_some());

        let dz_status = response.get("doublezero_status").unwrap();
        assert_eq!(dz_status.get("session_status").unwrap(), "BGP Session Up");
        assert_eq!(dz_status.get("last_session_update").unwrap(), 1_625_247_600);
        assert_eq!(status.get("current_device").unwrap(), "device1");
        assert_eq!(status.get("metro").unwrap(), "amsterdam");
    }

    /// Test JSON output format with null/missing optional fields.
    #[test]
    fn test_status_json_output_format_with_nulls() {
        let status_response = StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "PIM Adjacency Up".to_string(),
                last_session_update: None,
            },
            tunnel_name: Some("doublezero1".to_string()),
            tunnel_src: Some("10.0.0.1".to_string()),
            tunnel_dst: Some("5.6.7.8".to_string()),
            doublezero_ip: None,
            user_type: Some("Multicast".to_string()),
        };

        let appended_response = AppendedStatusResponse {
            response: status_response,
            reconciler_enabled: true,
            current_device: "device1".to_string(),
            lowest_latency_device: "device1".to_string(),
            metro: "amsterdam".to_string(),
            network: "Testnet".to_string(),
            tenant: "".to_string(),
            multicast_groups: String::new(),
            subscriptions: Vec::new(),
        };

        let json_response = vec![appended_response];
        let json_output = serde_json::to_value(&json_response).expect("Failed to serialize");
        let status = &json_output.as_array().unwrap()[0];
        let response = status.get("response").unwrap();

        assert!(response.get("doublezero_ip").unwrap().is_null());
        let dz_status = response.get("doublezero_status").unwrap();
        assert!(dz_status.get("last_session_update").unwrap().is_null());
        assert_eq!(response.get("user_type").unwrap(), "Multicast");
    }

    /// Test lowest_latency_device display formatting: json=true returns raw, json=false adds emoji.
    #[test]
    fn test_status_lowest_latency_display() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let ledger = MockLedgerClient::new();

            daemon.expect_v2_status().returning(|| {
                Ok(V2StatusResponse {
                    reconciler_enabled: true,
                    client_ip: String::new(),
                    network: "testnet".to_string(),
                    services: vec![make_v2_service(
                        "BGP Session Up",
                        Some("doublezero1"),
                        Some("1.2.3.4"),
                        Some("5.6.7.8"),
                        Some("10.0.0.1"),
                        Some("IBRL"),
                        "device1",
                        "device2",
                        "metro",
                        "",
                    )],
                })
            });

            // json=true: raw device code
            let result = Status { json: true }
                .build_status(&daemon, &ledger)
                .await
                .unwrap();
            assert_eq!(result[0].lowest_latency_device, "device2");

            // json=false, different devices: warning emoji
            let result = Status { json: false }
                .build_status(&daemon, &ledger)
                .await
                .unwrap();
            assert_eq!(result[0].lowest_latency_device, "⚠️ device2");
        });
    }

    #[test]
    fn test_format_multicast_groups() {
        assert_eq!(format_multicast_groups(&MulticastGroups::default()), "");
        assert_eq!(
            format_multicast_groups(&MulticastGroups {
                publisher: vec!["solana-lv".to_string()],
                subscriber: vec![],
            }),
            "P:solana-lv"
        );
        assert_eq!(
            format_multicast_groups(&MulticastGroups {
                publisher: vec![],
                subscriber: vec!["solana-ams".to_string()],
            }),
            "S:solana-ams"
        );
        assert_eq!(
            format_multicast_groups(&MulticastGroups {
                publisher: vec!["solana-lv".to_string()],
                subscriber: vec!["solana-ams".to_string(), "solana-fra".to_string()],
            }),
            "P:solana-lv,S:solana-ams,S:solana-fra"
        );
    }

    #[test]
    fn test_status_retries_transient_error() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            let ledger = MockLedgerClient::new();

            let calls = Arc::new(AtomicUsize::new(0));
            let calls_clone = calls.clone();
            daemon.expect_v2_status().returning(move || {
                if calls_clone.fetch_add(1, Ordering::SeqCst) < 2 {
                    Err(eyre::eyre!("Unable to connect to doublezero daemon: boom"))
                } else {
                    Ok(V2StatusResponse {
                        reconciler_enabled: true,
                        client_ip: String::new(),
                        network: "testnet".to_string(),
                        services: vec![make_v2_service(
                            "BGP Session Up",
                            Some("doublezero1"),
                            Some("1.2.3.4"),
                            Some("5.6.7.8"),
                            Some("10.0.0.1"),
                            Some("IBRL"),
                            "device1",
                            "device1",
                            "metro",
                            "",
                        )],
                    })
                }
            });

            let result = Status { json: true }.build_status(&daemon, &ledger).await;

            assert!(result.is_ok());
            assert_eq!(calls.load(Ordering::SeqCst), 3);
        });
    }

    #[test]
    fn test_status_daemon_not_running() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon.expect_daemon_check().return_const(false);
            let mut ledger = MockLedgerClient::new();
            ledger
                .expect_get_environment()
                .returning(Environment::default);

            let ctx = cli_context_default_for_tests();
            let mut out = Vec::new();
            let result = Status { json: false }
                .execute(&ctx, &daemon, &ledger, &mut out)
                .await;

            assert!(result.is_err());
        });
    }
}
