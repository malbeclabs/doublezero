use crate::{
    command::util,
    dzd_latency::best_latency,
    requirements::check_doublezero,
    servicecontroller::{ServiceController, ServiceControllerImpl, StatusResponse},
};
use clap::Args;
use doublezero_cli::{
    checkversion::{get_version_status, VersionStatus},
    doublezerocommand::CliCommand,
    helpers::print_error,
};
use doublezero_sdk::{
    commands::{
        device::list::ListDeviceCommand, exchange::list::ListExchangeCommand,
        user::list::ListUserCommand,
    },
    ProgramVersion,
};
use serde::{Deserialize, Serialize};
use solana_sdk::pubkey::Pubkey;
use std::{net::Ipv4Addr, str::FromStr};
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
    #[tabled(rename = "Current Device")]
    current_device: String,
    #[tabled(rename = "Lowest Latency Device")]
    lowest_latency_device: String,
    #[tabled(rename = "Metro")]
    metro: String,
    #[tabled(rename = "Network")]
    network: String,
}

/// JSON response wrapper that includes version status information
#[derive(Debug, Serialize, Deserialize)]
struct StatusJsonResponse {
    version: VersionStatus,
    statuses: Vec<AppendedStatusResponse>,
}

impl StatusCliCommand {
    pub async fn execute(&self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        check_doublezero(&controller, client, None).await?;

        // Get version status for JSON output
        let version_status = get_version_status(client, ProgramVersion::current());

        match self.command_impl(client, &controller).await {
            Ok(responses) => {
                if self.json {
                    // For JSON output, include version status in the response
                    let json_response = StatusJsonResponse {
                        version: version_status,
                        statuses: responses,
                    };
                    let output = serde_json::to_string_pretty(&json_response)?;
                    println!("{output}");
                } else {
                    // For table output, print version warning to stderr if needed
                    if let Some(msg) = version_status.message() {
                        eprintln!("{msg}");
                    }
                    util::show_output(responses, false)?;
                }
            }
            Err(e) => {
                if self.json {
                    // For JSON output, include error in a structured format
                    let error_response = serde_json::json!({
                        "version": version_status,
                        "error": e.to_string(),
                        "statuses": []
                    });
                    let output = serde_json::to_string_pretty(&error_response)?;
                    println!("{output}");
                } else {
                    print_error(e);
                }
            }
        }
        Ok(())
    }

    async fn command_impl<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
    ) -> eyre::Result<Vec<AppendedStatusResponse>> {
        let devices = client.list_device(ListDeviceCommand)?;
        let users = client.list_user(ListUserCommand)?;
        let exchanges = client.list_exchange(ListExchangeCommand)?;

        let status_responses = controller.status().await?;
        let mut responses = Vec::with_capacity(status_responses.len());
        for response in &status_responses {
            let mut current_device: Option<Pubkey> = None;
            let mut metro = None;
            let user = users
                .iter()
                .find(|(_, u)| {
                    let user_type_matches = response
                        .user_type
                        .as_ref()
                        .map(|t| u.user_type.to_string() == *t)
                        .unwrap_or(false);
                    if !user_type_matches {
                        return false;
                    }
                    // Match by dz_ip if doublezero_ip is present
                    if let Some(ref dz_ip) = response.doublezero_ip {
                        if !dz_ip.is_empty() {
                            return u.dz_ip.to_string() == *dz_ip;
                        }
                    }
                    false
                })
                .map(|(_, u)| u);
            if let Some(user) = user {
                current_device = Some(user.device_pk);
                if let Some(dev) = devices.get(&user.device_pk) {
                    metro = exchanges.get(&dev.exchange_pk).map(|e| e.name.clone());
                }
            } else if let Some(ref tunnel_dst) = response.tunnel_dst {
                // Fallback: match by tunnel_dst (device public IP) for users without dz_ip
                // This is needed for multicast subscribers who don't have a doublezero_ip
                if let Ok(tunnel_ip) = Ipv4Addr::from_str(tunnel_dst) {
                    if let Some((device_pk, dev)) =
                        devices.iter().find(|(_, d)| d.public_ip == tunnel_ip)
                    {
                        current_device = Some(*device_pk);
                        metro = exchanges.get(&dev.exchange_pk).map(|e| e.name.clone());
                    }
                }
            }
            let lowest_latency_device = match best_latency(
                controller,
                &devices,
                true,
                None,
                current_device.as_ref(),
                &[],
            )
            .await
            {
                Ok(best) => {
                    let is_current = current_device
                        .map(|d| best.device_pk == d.to_string())
                        .unwrap_or(false);
                    if self.json || response.doublezero_status.session_status != "BGP Session Up" {
                        best.device_code
                    } else if is_current {
                        format!("✅ {}", best.device_code)
                    } else if current_device.is_some() {
                        format!("⚠️ {}", best.device_code)
                    } else {
                        best.device_code
                    }
                }
                Err(_) => "N/A".to_string(),
            };

            responses.push(AppendedStatusResponse {
                response: response.clone(),
                current_device: current_device
                    .and_then(|d| devices.get(&d))
                    .map(|d| d.code.clone())
                    .unwrap_or_else(|| "N/A".to_string()),
                lowest_latency_device,
                metro: metro.unwrap_or_else(|| "N/A".to_string()),
                network: format!("{}", client.get_environment()),
            });
        }

        Ok(responses)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::servicecontroller::{DoubleZeroStatus, LatencyRecord, MockServiceController};
    use doublezero_cli::doublezerocommand::MockCliCommand;
    use doublezero_program_common::types::{NetworkV4, NetworkV4List};
    use doublezero_sdk::{
        AccountType, Device, DeviceStatus, DeviceType, Exchange, ExchangeStatus, User, UserCYOA,
        UserStatus, UserType,
    };
    use doublezero_serviceability::state::device::{DeviceDesiredStatus, DeviceHealth};
    use mockall::predicate::*;
    use solana_sdk::pubkey::Pubkey;

    #[tokio::test]
    async fn test_status_command_tunnel_up() {
        let mut mock_command = MockCliCommand::new();
        let mut mock_controller = MockServiceController::new();

        let mut devices = std::collections::HashMap::<Pubkey, Device>::new();
        let mut users = std::collections::HashMap::<Pubkey, User>::new();
        let mut exchanges = std::collections::HashMap::<Pubkey, Exchange>::new();
        let status_responses = vec![StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "BGP Session Up".to_string(),
                last_session_update: Some(1625247600),
            },
            tunnel_name: Some("tunnel_name".to_string()),
            tunnel_src: Some("1.2.3.4".to_string()),
            tunnel_dst: Some("42.42.42.42".to_string()),
            doublezero_ip: Some("1.2.3.4".to_string()),
            user_type: Some("IBRL".to_string()),
        }];

        let exchange_pk = Pubkey::new_unique();
        exchanges.insert(
            exchange_pk,
            Exchange {
                account_type: AccountType::Exchange,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                lat: 0.0,
                lng: 0.0,
                bgp_community: 0,
                unused: 0,
                status: ExchangeStatus::Activated,
                code: "met".to_string(),
                name: "metro".to_string(),
                reference_count: 0,
                device1_pk: Pubkey::default(),
                device2_pk: Pubkey::default(),
            },
        );

        let device1_pk = Pubkey::new_unique();
        devices.insert(
            device1_pk,
            Device {
                account_type: AccountType::Device,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                location_pk: Pubkey::default(),
                exchange_pk,
                device_type: DeviceType::Hybrid,
                public_ip: "5.6.7.8".parse().unwrap(),
                status: DeviceStatus::Activated,
                code: "device1".to_string(),
                dz_prefixes: NetworkV4List::default(),
                metrics_publisher_pk: Pubkey::default(),
                contributor_pk: Pubkey::default(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                reference_count: 0,
                users_count: 64,
                max_users: 128,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            },
        );

        let device2_pk = Pubkey::new_unique();
        devices.insert(
            device2_pk,
            Device {
                account_type: AccountType::Device,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                location_pk: Pubkey::default(),
                exchange_pk,
                device_type: DeviceType::Hybrid,
                public_ip: "5.6.7.9".parse().unwrap(),
                status: DeviceStatus::Activated,
                code: "device2".to_string(),
                dz_prefixes: NetworkV4List::default(),
                metrics_publisher_pk: Pubkey::default(),
                contributor_pk: Pubkey::default(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                reference_count: 0,
                users_count: 64,
                max_users: 128,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            },
        );

        let user_pk = Pubkey::new_unique();
        users.insert(
            user_pk,
            User {
                account_type: AccountType::User,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                user_type: UserType::IBRL,
                tenant_pk: Pubkey::default(),
                device_pk: device1_pk,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: "1.2.3.4".parse().unwrap(),
                dz_ip: "1.2.3.4".parse().unwrap(),
                tunnel_id: 501,
                tunnel_net: NetworkV4::default(),
                status: UserStatus::Activated,
                publishers: vec![],
                subscribers: vec![],
                validator_pubkey: Pubkey::default(),
            },
        );

        let latencies = vec![
            LatencyRecord {
                device_pk: device1_pk.to_string(),
                device_code: "device1".to_string(),
                device_ip: "5.6.7.8".to_string(),
                min_latency_ns: 10000000,
                max_latency_ns: 10000000,
                avg_latency_ns: 10000000,
                reachable: true,
            },
            LatencyRecord {
                device_pk: device2_pk.to_string(),
                device_code: "device2".to_string(),
                device_ip: "5.6.7.9".to_string(),
                min_latency_ns: 3000000,
                max_latency_ns: 3000000,
                avg_latency_ns: 3000000,
                reachable: true,
            },
        ];

        mock_controller
            .expect_status()
            .returning(move || Ok(status_responses.clone()));
        mock_controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));
        mock_command
            .expect_get_environment()
            .return_const(doublezero_config::Environment::Testnet);
        mock_command
            .expect_list_device()
            .with(eq(ListDeviceCommand))
            .returning({
                let devices = devices.clone();
                move |_| Ok(devices.clone())
            });
        mock_command
            .expect_list_user()
            .with(eq(ListUserCommand))
            .returning({
                let users = users.clone();
                move |_| Ok(users.clone())
            });
        mock_command
            .expect_list_exchange()
            .with(eq(ListExchangeCommand))
            .returning({
                let exchanges = exchanges.clone();
                move |_| Ok(exchanges.clone())
            });

        let result = StatusCliCommand { json: true }
            .command_impl(&mock_command, &mock_controller)
            .await;

        // Assert that the result is Ok
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
        assert_eq!(result[0].current_device, "device1".to_string());
        assert_eq!(result[0].lowest_latency_device, "device2".to_string());
        assert_eq!(result[0].metro, "metro".to_string());
        assert_eq!(result[0].network, "testnet".to_string());
    }

    #[tokio::test]
    async fn test_status_command_tunnel_down() {
        let mut mock_command = MockCliCommand::new();
        let mut mock_controller = MockServiceController::new();

        let mut devices = std::collections::HashMap::<Pubkey, Device>::new();
        let users = std::collections::HashMap::<Pubkey, User>::new();
        let mut exchanges = std::collections::HashMap::<Pubkey, Exchange>::new();
        let status_responses = vec![StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "BGP Session Down".to_string(),
                last_session_update: None,
            },
            tunnel_name: None,
            tunnel_src: None,
            tunnel_dst: None,
            doublezero_ip: None,
            user_type: None,
        }];

        let exchange_pk = Pubkey::new_unique();
        exchanges.insert(
            exchange_pk,
            Exchange {
                account_type: AccountType::Exchange,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                lat: 0.0,
                lng: 0.0,
                bgp_community: 0,
                unused: 0,
                status: ExchangeStatus::Activated,
                code: "met".to_string(),
                name: "metro".to_string(),
                reference_count: 0,
                device1_pk: Pubkey::default(),
                device2_pk: Pubkey::default(),
            },
        );

        let device1_pk = Pubkey::new_unique();
        devices.insert(
            device1_pk,
            Device {
                account_type: AccountType::Device,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                location_pk: Pubkey::default(),
                exchange_pk,
                device_type: DeviceType::Hybrid,
                public_ip: "5.6.7.8".parse().unwrap(),
                status: DeviceStatus::Activated,
                code: "device1".to_string(),
                dz_prefixes: NetworkV4List::default(),
                metrics_publisher_pk: Pubkey::default(),
                contributor_pk: Pubkey::default(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                reference_count: 0,
                users_count: 64,
                max_users: 128,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            },
        );

        let device2_pk = Pubkey::new_unique();
        devices.insert(
            device2_pk,
            Device {
                account_type: AccountType::Device,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                location_pk: Pubkey::default(),
                exchange_pk,
                device_type: DeviceType::Hybrid,
                public_ip: "5.6.7.9".parse().unwrap(),
                status: DeviceStatus::Activated,
                code: "device2".to_string(),
                dz_prefixes: NetworkV4List::default(),
                metrics_publisher_pk: Pubkey::default(),
                contributor_pk: Pubkey::default(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                reference_count: 0,
                users_count: 64,
                max_users: 128,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            },
        );

        let latencies = vec![
            LatencyRecord {
                device_pk: device1_pk.to_string(),
                device_code: "device1".to_string(),
                device_ip: "5.6.7.8".to_string(),
                min_latency_ns: 5000000,
                max_latency_ns: 5000000,
                avg_latency_ns: 5000000,
                reachable: true,
            },
            LatencyRecord {
                device_pk: device2_pk.to_string(),
                device_code: "device2".to_string(),
                device_ip: "5.6.7.9".to_string(),
                min_latency_ns: 3000000,
                max_latency_ns: 3000000,
                avg_latency_ns: 3000000,
                reachable: true,
            },
        ];

        mock_controller
            .expect_status()
            .returning(move || Ok(status_responses.clone()));
        mock_controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));
        mock_command
            .expect_get_environment()
            .return_const(doublezero_config::Environment::Testnet);
        mock_command
            .expect_list_device()
            .with(eq(ListDeviceCommand))
            .returning({
                let devices = devices.clone();
                move |_| Ok(devices.clone())
            });
        mock_command
            .expect_list_user()
            .with(eq(ListUserCommand))
            .returning({
                let users = users.clone();
                move |_| Ok(users.clone())
            });
        mock_command
            .expect_list_exchange()
            .with(eq(ListExchangeCommand))
            .returning({
                let exchanges = exchanges.clone();
                move |_| Ok(exchanges.clone())
            });

        let result = StatusCliCommand { json: true }
            .command_impl(&mock_command, &mock_controller)
            .await;

        // Assert that the result is Ok
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
        assert_eq!(result[0].current_device, "N/A".to_string());
        assert_eq!(result[0].lowest_latency_device, "device2".to_string());
        assert_eq!(result[0].metro, "N/A".to_string());
        assert_eq!(result[0].network, "testnet".to_string());
    }

    #[tokio::test]
    async fn test_status_command_matches_dz_ip_not_client_ip() {
        let mut mock_command = MockCliCommand::new();
        let mut mock_controller = MockServiceController::new();

        let mut devices = std::collections::HashMap::<Pubkey, Device>::new();
        let mut users = std::collections::HashMap::<Pubkey, User>::new();
        let mut exchanges = std::collections::HashMap::<Pubkey, Exchange>::new();
        let status_responses = vec![StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "BGP Session Up".to_string(),
                last_session_update: Some(1625247600),
            },
            tunnel_name: Some("tunnel_name".to_string()),
            tunnel_src: Some("20.20.20.20".to_string()),
            tunnel_dst: Some("42.42.42.42".to_string()),
            doublezero_ip: Some("1.2.3.4".to_string()),
            user_type: Some("IBRL".to_string()),
        }];

        let exchange_pk = Pubkey::new_unique();
        exchanges.insert(
            exchange_pk,
            Exchange {
                account_type: AccountType::Exchange,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                lat: 0.0,
                lng: 0.0,
                bgp_community: 0,
                unused: 0,
                status: ExchangeStatus::Activated,
                code: "met".to_string(),
                name: "metro".to_string(),
                reference_count: 0,
                device1_pk: Pubkey::default(),
                device2_pk: Pubkey::default(),
            },
        );

        let device1_pk = Pubkey::new_unique();
        devices.insert(
            device1_pk,
            Device {
                account_type: AccountType::Device,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                location_pk: Pubkey::default(),
                exchange_pk,
                device_type: DeviceType::Hybrid,
                public_ip: "5.6.7.8".parse().unwrap(),
                status: DeviceStatus::Activated,
                code: "device1".to_string(),
                dz_prefixes: NetworkV4List::default(),
                metrics_publisher_pk: Pubkey::default(),
                contributor_pk: Pubkey::default(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                reference_count: 0,
                users_count: 64,
                max_users: 128,
                desired_status: DeviceDesiredStatus::Activated,
                device_health: DeviceHealth::ReadyForUsers,
            },
        );

        let user_pk = Pubkey::new_unique();
        users.insert(
            user_pk,
            User {
                account_type: AccountType::User,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                user_type: UserType::IBRL,
                tenant_pk: Pubkey::default(),
                device_pk: device1_pk,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: "10.10.10.10".parse().unwrap(),
                dz_ip: "1.2.3.4".parse().unwrap(),
                tunnel_id: 501,
                tunnel_net: NetworkV4::default(),
                status: UserStatus::Activated,
                publishers: vec![],
                subscribers: vec![],
                validator_pubkey: Pubkey::default(),
            },
        );

        let latencies = vec![LatencyRecord {
            device_pk: device1_pk.to_string(),
            device_code: "device1".to_string(),
            device_ip: "5.6.7.8".to_string(),
            min_latency_ns: 5000000,
            max_latency_ns: 5000000,
            avg_latency_ns: 5000000,
            reachable: true,
        }];

        mock_controller
            .expect_status()
            .returning(move || Ok(status_responses.clone()));
        mock_controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));
        mock_command
            .expect_get_environment()
            .return_const(doublezero_config::Environment::Testnet);
        mock_command
            .expect_list_device()
            .with(eq(ListDeviceCommand))
            .returning({
                let devices = devices.clone();
                move |_| Ok(devices.clone())
            });
        mock_command
            .expect_list_user()
            .with(eq(ListUserCommand))
            .returning({
                let users = users.clone();
                move |_| Ok(users.clone())
            });
        mock_command
            .expect_list_exchange()
            .with(eq(ListExchangeCommand))
            .returning({
                let exchanges = exchanges.clone();
                move |_| Ok(exchanges.clone())
            });

        let result = StatusCliCommand { json: true }
            .command_impl(&mock_command, &mock_controller)
            .await;

        assert!(result.is_ok());
        let result = result.unwrap();
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].current_device, "device1".to_string());
        assert_eq!(result[0].metro, "metro".to_string());
    }

    /// Test that multicast subscribers (which have no dz_ip) can still be matched
    /// to their device via tunnel_dst (device public IP) fallback
    #[tokio::test]
    async fn test_status_command_multicast_subscriber_matches_by_tunnel_dst() {
        let mut mock_command = MockCliCommand::new();
        let mut mock_controller = MockServiceController::new();

        let mut devices = std::collections::HashMap::<Pubkey, Device>::new();
        let users = std::collections::HashMap::<Pubkey, User>::new(); // No users - subscriber has no dz_ip
        let mut exchanges = std::collections::HashMap::<Pubkey, Exchange>::new();

        // Multicast subscriber: has tunnel_dst but no doublezero_ip
        let status_responses = vec![StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "BGP Session Up".to_string(),
                last_session_update: Some(1625247600),
            },
            tunnel_name: Some("doublezero1".to_string()),
            tunnel_src: Some("10.10.10.10".to_string()),
            tunnel_dst: Some("5.6.7.8".to_string()), // Device public IP
            doublezero_ip: None,                     // Subscribers don't have dz_ip
            user_type: Some("Multicast".to_string()),
        }];

        let exchange_pk = Pubkey::new_unique();
        exchanges.insert(
            exchange_pk,
            Exchange {
                account_type: AccountType::Exchange,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                lat: 0.0,
                lng: 0.0,
                bgp_community: 0,
                unused: 0,
                status: ExchangeStatus::Activated,
                code: "met".to_string(),
                name: "metro".to_string(),
                reference_count: 0,
                device1_pk: Pubkey::default(),
                device2_pk: Pubkey::default(),
            },
        );

        let device1_pk = Pubkey::new_unique();
        devices.insert(
            device1_pk,
            Device {
                account_type: AccountType::Device,
                owner: Pubkey::default(),
                index: 0,
                bump_seed: 0,
                location_pk: Pubkey::default(),
                exchange_pk,
                device_type: DeviceType::Hybrid,
                public_ip: "5.6.7.8".parse().unwrap(), // Matches tunnel_dst
                status: DeviceStatus::Activated,
                code: "device1".to_string(),
                dz_prefixes: NetworkV4List::default(),
                metrics_publisher_pk: Pubkey::default(),
                contributor_pk: Pubkey::default(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                reference_count: 0,
                users_count: 64,
                max_users: 128,
                device_health: DeviceHealth::ReadyForUsers,
                desired_status: DeviceDesiredStatus::Activated,
            },
        );

        let latencies = vec![LatencyRecord {
            device_pk: device1_pk.to_string(),
            device_code: "device1".to_string(),
            device_ip: "5.6.7.8".to_string(),
            min_latency_ns: 5000000,
            max_latency_ns: 5000000,
            avg_latency_ns: 5000000,
            reachable: true,
        }];

        mock_controller
            .expect_status()
            .returning(move || Ok(status_responses.clone()));
        mock_controller
            .expect_latency()
            .returning(move || Ok(latencies.clone()));
        mock_command
            .expect_get_environment()
            .return_const(doublezero_config::Environment::Testnet);
        mock_command
            .expect_list_device()
            .with(eq(ListDeviceCommand))
            .returning({
                let devices = devices.clone();
                move |_| Ok(devices.clone())
            });
        mock_command
            .expect_list_user()
            .with(eq(ListUserCommand))
            .returning({
                let users = users.clone();
                move |_| Ok(users.clone())
            });
        mock_command
            .expect_list_exchange()
            .with(eq(ListExchangeCommand))
            .returning({
                let exchanges = exchanges.clone();
                move |_| Ok(exchanges.clone())
            });

        let result = StatusCliCommand { json: true }
            .command_impl(&mock_command, &mock_controller)
            .await;

        assert!(result.is_ok());
        let result = result.unwrap();
        assert_eq!(result.len(), 1);
        // Should match device by tunnel_dst even though there's no dz_ip
        assert_eq!(result[0].current_device, "device1".to_string());
        assert_eq!(result[0].metro, "metro".to_string());
        // Should show checkmark since current_device matches lowest_latency_device
        assert_eq!(result[0].lowest_latency_device, "device1".to_string());
    }
}
