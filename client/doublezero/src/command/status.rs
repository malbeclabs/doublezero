use crate::{
    command::util,
    requirements::check_doublezero,
    servicecontroller::{
        DoubleZeroStatus, ServiceController, ServiceControllerImpl, StatusResponse,
    },
};
use clap::Args;
use doublezero_cli::{doublezerocommand::CliCommand, helpers::print_error};
use serde::{Deserialize, Serialize};
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct StatusCliCommand {
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
}

impl StatusCliCommand {
    pub async fn execute(&self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        check_doublezero(&controller, client, None).await?;
        match self.command_impl(client, &controller).await {
            Ok(responses) => util::show_output(responses, self.json)?,
            Err(e) => {
                print_error(e);
            }
        }
        Ok(())
    }

    async fn command_impl<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
    ) -> eyre::Result<Vec<AppendedStatusResponse>> {
        let v2_status = controller.v2_status().await?;

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
                    format!("{}", client.get_environment())
                } else {
                    v2_status.network.clone()
                },
            }]);
        }

        let network = if v2_status.network.is_empty() {
            format!("{}", client.get_environment())
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
            });
        }

        Ok(responses)
    }
}

// NOTE: if the client is out of date, there is an error because the client warning will cause the json to be malformed. This was resolved in this PR (https://github.com/malbeclabs/doublezero/pull/2807) but the global monitor and maybe other things will break so these tests capture the expected format. The json response should be fixed sooner than later.
#[cfg(test)]
mod tests {
    use super::*;
    use crate::servicecontroller::{
        DoubleZeroStatus, MockServiceController, V2ServiceStatus, V2StatusResponse,
    };
    use doublezero_cli::doublezerocommand::MockCliCommand;

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
                    last_session_update: Some(1625247600),
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
        }
    }

    #[tokio::test]
    async fn test_status_command_tunnel_up() {
        let mock_command = MockCliCommand::new();
        let mut mock_controller = MockServiceController::new();

        mock_controller.expect_v2_status().returning(|| {
            Ok(V2StatusResponse {
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
            })
        });

        let result = StatusCliCommand { json: true }
            .command_impl(&mock_command, &mock_controller)
            .await;

        assert!(result.is_ok());
        let result = result.unwrap();
        assert_eq!(result.len(), 1);
        let status_response = &result[0].response;
        assert_eq!(
            status_response.doublezero_status.session_status,
            "BGP Session Up"
        );
        assert_eq!(status_response.tunnel_name.as_deref(), Some("tunnel_name"));
        assert_eq!(status_response.tunnel_src.as_deref(), Some("1.2.3.4"));
        assert_eq!(status_response.tunnel_dst.as_deref(), Some("42.42.42.42"));
        assert_eq!(status_response.doublezero_ip.as_deref(), Some("1.2.3.4"));
        assert_eq!(status_response.user_type.as_deref(), Some("IBRL"));
        assert_eq!(result[0].current_device, "device1");
        assert_eq!(result[0].lowest_latency_device, "device2");
        assert_eq!(result[0].metro, "metro");
        assert_eq!(result[0].network, "testnet");
        assert_eq!(result[0].tenant, "");
    }

    #[tokio::test]
    async fn test_status_command_tunnel_down() {
        let mock_command = MockCliCommand::new();
        let mut mock_controller = MockServiceController::new();

        mock_controller.expect_v2_status().returning(|| {
            Ok(V2StatusResponse {
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
                }],
            })
        });

        let result = StatusCliCommand { json: true }
            .command_impl(&mock_command, &mock_controller)
            .await;

        assert!(result.is_ok());
        let result = result.unwrap();
        assert_eq!(result.len(), 1);
        let status_response = &result[0].response;
        assert_eq!(
            status_response.doublezero_status.session_status,
            "BGP Session Down"
        );
        assert_eq!(status_response.tunnel_name.as_deref(), None);
        assert_eq!(status_response.tunnel_src.as_deref(), None);
        assert_eq!(status_response.tunnel_dst.as_deref(), None);
        assert_eq!(status_response.doublezero_ip.as_deref(), None);
        assert_eq!(status_response.user_type.as_deref(), None);
        assert_eq!(result[0].current_device, "N/A");
        assert_eq!(result[0].lowest_latency_device, "device2");
        assert_eq!(result[0].metro, "N/A");
        assert_eq!(result[0].network, "testnet");
        assert_eq!(result[0].tenant, "");
    }

    #[tokio::test]
    async fn test_status_command_enriched_from_daemon() {
        let mock_command = MockCliCommand::new();
        let mut mock_controller = MockServiceController::new();

        mock_controller.expect_v2_status().returning(|| {
            Ok(V2StatusResponse {
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
            })
        });

        let result = StatusCliCommand { json: true }
            .command_impl(&mock_command, &mock_controller)
            .await;

        assert!(result.is_ok());
        let result = result.unwrap();
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].current_device, "device1");
        assert_eq!(result[0].metro, "metro");
    }

    #[tokio::test]
    async fn test_status_command_multicast_subscriber() {
        let mock_command = MockCliCommand::new();
        let mut mock_controller = MockServiceController::new();

        mock_controller.expect_v2_status().returning(|| {
            Ok(V2StatusResponse {
                reconciler_enabled: true,
                client_ip: String::new(),
                network: "testnet".to_string(),
                services: vec![V2ServiceStatus {
                    status: StatusResponse {
                        doublezero_status: DoubleZeroStatus {
                            session_status: "BGP Session Up".to_string(),
                            last_session_update: Some(1625247600),
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
                }],
            })
        });

        let result = StatusCliCommand { json: true }
            .command_impl(&mock_command, &mock_controller)
            .await;

        assert!(result.is_ok());
        let result = result.unwrap();
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].current_device, "device1");
        assert_eq!(result[0].metro, "metro");
        assert_eq!(result[0].lowest_latency_device, "device1");
    }

    /// Test that validates the JSON output format for the status command.
    /// This test catches breaking changes to the JSON API contract.
    /// The JSON output is an array of AppendedStatusResponse objects.
    #[test]
    fn test_status_json_output_format() {
        use crate::servicecontroller::DoubleZeroStatus;

        // Create a sample StatusResponse
        let status_response = StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "BGP Session Up".to_string(),
                last_session_update: Some(1625247600),
            },
            tunnel_name: Some("doublezero1".to_string()),
            tunnel_src: Some("10.0.0.1".to_string()),
            tunnel_dst: Some("5.6.7.8".to_string()),
            doublezero_ip: Some("10.1.2.3".to_string()),
            user_type: Some("IBRL".to_string()),
        };

        // Create AppendedStatusResponse
        let appended_response = AppendedStatusResponse {
            response: status_response,
            reconciler_enabled: true,
            current_device: "device1".to_string(),
            lowest_latency_device: "device1".to_string(),
            metro: "amsterdam".to_string(),
            network: "Testnet".to_string(),
            tenant: "".to_string(),
        };

        // JSON output is an array of status responses
        let json_response = vec![appended_response];

        // Serialize to JSON
        let json_output = serde_json::to_value(&json_response).expect("Failed to serialize");

        // Validate top-level structure is an array
        assert!(json_output.is_array(), "Response should be an array");
        assert_eq!(json_output.as_array().unwrap().len(), 1);

        // Validate status entry fields
        let status = &json_output.as_array().unwrap()[0];
        assert!(status.get("response").is_some(), "Missing 'response' field");
        assert!(
            status.get("reconciler_enabled").is_some(),
            "Missing 'reconciler_enabled' field"
        );
        assert!(
            status.get("current_device").is_some(),
            "Missing 'current_device' field"
        );
        assert!(
            status.get("lowest_latency_device").is_some(),
            "Missing 'lowest_latency_device' field"
        );
        assert!(status.get("metro").is_some(), "Missing 'metro' field");
        assert!(status.get("network").is_some(), "Missing 'network' field");
        assert!(status.get("tenant").is_some(), "Missing 'tenant' field");

        // Validate response nested fields
        let response = status.get("response").unwrap();
        assert!(
            response.get("doublezero_status").is_some(),
            "Missing 'doublezero_status' field"
        );
        assert!(
            response.get("tunnel_name").is_some(),
            "Missing 'tunnel_name' field"
        );
        assert!(
            response.get("tunnel_src").is_some(),
            "Missing 'tunnel_src' field"
        );
        assert!(
            response.get("tunnel_dst").is_some(),
            "Missing 'tunnel_dst' field"
        );
        assert!(
            response.get("doublezero_ip").is_some(),
            "Missing 'doublezero_ip' field"
        );
        assert!(
            response.get("user_type").is_some(),
            "Missing 'user_type' field"
        );

        // Validate doublezero_status nested fields
        let dz_status = response.get("doublezero_status").unwrap();
        assert!(
            dz_status.get("session_status").is_some(),
            "Missing 'session_status' field"
        );
        assert!(
            dz_status.get("last_session_update").is_some(),
            "Missing 'last_session_update' field"
        );

        // Validate field values
        assert_eq!(status.get("current_device").unwrap(), "device1");
        assert_eq!(status.get("lowest_latency_device").unwrap(), "device1");
        assert_eq!(status.get("metro").unwrap(), "amsterdam");
        assert_eq!(status.get("network").unwrap(), "Testnet");
        assert_eq!(response.get("tunnel_name").unwrap(), "doublezero1");
        assert_eq!(response.get("tunnel_src").unwrap(), "10.0.0.1");
        assert_eq!(response.get("tunnel_dst").unwrap(), "5.6.7.8");
        assert_eq!(response.get("doublezero_ip").unwrap(), "10.1.2.3");
        assert_eq!(response.get("user_type").unwrap(), "IBRL");
        assert_eq!(dz_status.get("session_status").unwrap(), "BGP Session Up");
        assert_eq!(dz_status.get("last_session_update").unwrap(), 1625247600);
    }

    /// Test JSON output format with null/missing optional fields
    #[test]
    fn test_status_json_output_format_with_nulls() {
        use crate::servicecontroller::DoubleZeroStatus;

        // Create a StatusResponse with None values (e.g., multicast subscriber without dz_ip)
        let status_response = StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "PIM Adjacency Up".to_string(),
                last_session_update: None,
            },
            tunnel_name: Some("doublezero1".to_string()),
            tunnel_src: Some("10.0.0.1".to_string()),
            tunnel_dst: Some("5.6.7.8".to_string()),
            doublezero_ip: None, // Multicast subscribers don't have dz_ip
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
        };

        // JSON output is an array of status responses
        let json_response = vec![appended_response];

        let json_output = serde_json::to_value(&json_response).expect("Failed to serialize");

        // Validate that null fields are properly serialized
        let status = &json_output.as_array().unwrap()[0];
        let response = status.get("response").unwrap();

        // doublezero_ip should be null
        assert!(
            response.get("doublezero_ip").is_some(),
            "doublezero_ip field should exist"
        );
        assert!(
            response.get("doublezero_ip").unwrap().is_null(),
            "doublezero_ip should be null"
        );

        // last_session_update should be null
        let dz_status = response.get("doublezero_status").unwrap();
        assert!(
            dz_status.get("last_session_update").unwrap().is_null(),
            "last_session_update should be null"
        );

        // user_type should still be present
        assert_eq!(response.get("user_type").unwrap(), "Multicast");
    }

    #[tokio::test]
    async fn test_status_reconciler_disabled() {
        let mock_command = MockCliCommand::new();
        let mut mock_controller = MockServiceController::new();

        mock_controller.expect_v2_status().returning(|| {
            Ok(V2StatusResponse {
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
            })
        });

        let result = StatusCliCommand { json: true }
            .command_impl(&mock_command, &mock_controller)
            .await;

        assert!(result.is_ok());
        let result = result.unwrap();
        assert_eq!(result.len(), 1);
        assert!(!result[0].reconciler_enabled);
    }

    #[tokio::test]
    async fn test_status_empty_services_disconnected() {
        let mut mock_command = MockCliCommand::new();
        let mut mock_controller = MockServiceController::new();

        mock_controller.expect_v2_status().returning(|| {
            Ok(V2StatusResponse {
                reconciler_enabled: false,
                client_ip: String::new(),
                network: "testnet".to_string(),
                services: vec![],
            })
        });
        mock_command
            .expect_get_environment()
            .return_const(doublezero_config::Environment::Testnet);

        let result = StatusCliCommand { json: true }
            .command_impl(&mock_command, &mock_controller)
            .await;

        assert!(result.is_ok());
        let result = result.unwrap();
        // When no services are running, a synthetic "disconnected" entry is returned
        assert_eq!(result.len(), 1);
        assert_eq!(
            result[0].response.doublezero_status.session_status,
            "disconnected"
        );
        assert!(!result[0].reconciler_enabled);
        assert_eq!(result[0].current_device, "N/A");
        assert_eq!(result[0].network, "testnet");
    }

    /// Test that the lowest_latency_device display formatting works correctly.
    /// When session is up, json=true returns raw device code.
    #[tokio::test]
    async fn test_status_lowest_latency_display_json() {
        let mock_command = MockCliCommand::new();
        let mut mock_controller = MockServiceController::new();

        mock_controller.expect_v2_status().returning(|| {
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
                    "device2", // different from current
                    "metro",
                    "",
                )],
            })
        });

        // json=true: raw device code, no emoji
        let result = StatusCliCommand { json: true }
            .command_impl(&mock_command, &mock_controller)
            .await
            .unwrap();
        assert_eq!(result[0].lowest_latency_device, "device2");

        // json=false: should get warning emoji since lowest != current
        let mut mock_controller2 = MockServiceController::new();
        mock_controller2.expect_v2_status().returning(|| {
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
        let result = StatusCliCommand { json: false }
            .command_impl(&mock_command, &mock_controller2)
            .await
            .unwrap();
        assert_eq!(result[0].lowest_latency_device, "⚠️ device2");
    }
}
