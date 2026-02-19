use crate::{
    command::util,
    dzd_latency::best_latency,
    requirements::check_doublezero,
    servicecontroller::{ServiceController, ServiceControllerImpl, StatusResponse},
};
use clap::Args;
use doublezero_cli::{doublezerocommand::CliCommand, helpers::print_error};
use doublezero_sdk::commands::{
    device::list::ListDeviceCommand, exchange::list::ListExchangeCommand,
    tenant::list::ListTenantCommand, user::list::ListUserCommand,
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
        let devices = client.list_device(ListDeviceCommand)?;
        let users = client.list_user(ListUserCommand)?;
        let exchanges = client.list_exchange(ListExchangeCommand)?;
        let tenants = client.list_tenant(ListTenantCommand {})?;

        let status_responses = controller.status().await?;
        let mut responses = Vec::with_capacity(status_responses.len());
        for response in &status_responses {
            let mut current_device: Option<Pubkey> = None;
            let mut metro = None;
            let mut tenant_code = String::new();
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
                if user.tenant_pk != Pubkey::default() {
                    tenant_code = tenants
                        .get(&user.tenant_pk)
                        .map(|t| t.code.clone())
                        .unwrap_or_default();
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
                tenant: tenant_code,
            });
        }

        Ok(responses)
    }
}

// NOTE: if the client is out of date, there is an error because the client warning will cause the json to be malformed. This was resolved in this PR (https://github.com/malbeclabs/doublezero/pull/2807) but the global monitor and maybe other things will break so these tests capture the expected format. The json response should be fixed sooner than later.
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
    use doublezero_serviceability::state::{
        device::{DeviceDesiredStatus, DeviceHealth},
        tenant::Tenant,
    };
    use mockall::predicate::*;
    use solana_sdk::pubkey::Pubkey;
    use std::net::Ipv4Addr;

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
                unicast_users_count: 0,
                multicast_users_count: 0,
                max_unicast_users: 0,
                max_multicast_users: 0,
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
                unicast_users_count: 0,
                multicast_users_count: 0,
                max_unicast_users: 0,
                max_multicast_users: 0,
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
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
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
        mock_command
            .expect_list_tenant()
            .with(eq(ListTenantCommand {}))
            .returning(|_| Ok(std::collections::HashMap::<Pubkey, Tenant>::new()));

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
        assert_eq!(result[0].tenant, "".to_string());
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
                unicast_users_count: 0,
                multicast_users_count: 0,
                max_unicast_users: 0,
                max_multicast_users: 0,
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
                unicast_users_count: 0,
                multicast_users_count: 0,
                max_unicast_users: 0,
                max_multicast_users: 0,
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
        mock_command
            .expect_list_tenant()
            .with(eq(ListTenantCommand {}))
            .returning(|_| Ok(std::collections::HashMap::<Pubkey, Tenant>::new()));

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
        assert_eq!(result[0].tenant, "".to_string());
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
                unicast_users_count: 0,
                multicast_users_count: 0,
                max_unicast_users: 0,
                max_multicast_users: 0,
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
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
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
        mock_command
            .expect_list_tenant()
            .with(eq(ListTenantCommand {}))
            .returning(|_| Ok(std::collections::HashMap::<Pubkey, Tenant>::new()));

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
                unicast_users_count: 0,
                multicast_users_count: 0,
                max_unicast_users: 0,
                max_multicast_users: 0,
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
        mock_command
            .expect_list_tenant()
            .with(eq(ListTenantCommand {}))
            .returning(|_| Ok(std::collections::HashMap::<Pubkey, Tenant>::new()));

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
}
