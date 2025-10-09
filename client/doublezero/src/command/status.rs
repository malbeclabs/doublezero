use crate::{
    command::util,
    requirements::check_doublezero,
    servicecontroller::{display_as_ms, LatencyRecord, ServiceController, ServiceControllerImpl},
};
use clap::Args;
use doublezero_cli::{doublezerocommand::CliCommand, helpers::print_error};
use doublezero_sdk::commands::device::list::ListDeviceCommand;

// Thresholds for device recommendation
const LATENCY_THRESHOLD_NS: i32 = 10_000_000; // 10ms - devices within this latency are considered similar
const LOAD_DIFFERENCE_THRESHOLD: f32 = 0.2; // 20% - minimum load difference to recommend switching

#[derive(Args, Debug)]
pub struct StatusCliCommand {
    /// Output as json
    #[arg(long, default_value = "false")]
    json: bool,
}

impl StatusCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        self.execute_with_service_controller(client, &controller)
            .await
    }

    pub async fn execute_with_service_controller<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
    ) -> eyre::Result<()> {
        // Check requirements
        check_doublezero(controller, client, None).await?;

        let status_responses = controller.status().await?;

        if status_responses.is_empty() {
            util::show_output(status_responses, self.json)?;
            return Ok(());
        }

        util::show_output(status_responses.clone(), self.json)?;

        for status in &status_responses {
            // Filter for the main IBRL/unicast connection
            if status.user_type.as_deref() == Some("ibrl") {
                if let Some(device_ip) = &status.tunnel_dst {
                    // Check if there is a better connection to another DZD
                    let latencies: Vec<LatencyRecord> = controller.latency().await?;
                    let devices = client.list_device(ListDeviceCommand)?;

                    let current_latency = latencies
                        .iter()
                        .find(|l| &l.device_ip == device_ip && l.reachable)
                        .map(|l| l.avg_latency_ns);

                    let current_device = devices
                        .values()
                        .find(|d| &d.public_ip.to_string() == device_ip);

                    if let (Some(current_avg), Some(current_dev)) =
                        (current_latency, current_device)
                    {
                        let current_load = if current_dev.max_users == 0 {
                            0.0
                        } else {
                            current_dev.users_count as f32 / current_dev.max_users as f32
                        };

                        let best_device = latencies
                            .iter()
                            .filter(|l| l.reachable && &l.device_ip != device_ip)
                            .filter_map(|l| {
                                // Find matching device info
                                devices
                                    .values()
                                    .find(|d| d.public_ip.to_string() == l.device_ip)
                                    .map(|d| (l, d))
                            })
                            .filter(|(l, d)| {
                                if d.max_users == 0 {
                                    return false;
                                }
                                let load = d.users_count as f32 / d.max_users as f32;
                                let latency_diff = (l.avg_latency_ns - current_avg).abs();

                                // Better if: lower latency OR similar latency but much less load
                                l.avg_latency_ns < current_avg
                                    || (latency_diff < LATENCY_THRESHOLD_NS
                                        && load < current_load - LOAD_DIFFERENCE_THRESHOLD)
                            })
                            .min_by(|(l1, d1), (l2, d2)| {
                                // Sort by latency first, then load
                                l1.avg_latency_ns.cmp(&l2.avg_latency_ns).then_with(|| {
                                    if d1.max_users == 0 || d2.max_users == 0 {
                                        return std::cmp::Ordering::Equal;
                                    }
                                    let load1 = d1.users_count as f32 / d1.max_users as f32;
                                    let load2 = d2.users_count as f32 / d2.max_users as f32;
                                    load1
                                        .partial_cmp(&load2)
                                        .unwrap_or(std::cmp::Ordering::Equal)
                                })
                            });

                        if let Some((better_latency, better_device)) = best_device {
                            let current_ms = display_as_ms(&current_avg);
                            let better_ms = display_as_ms(&better_latency.avg_latency_ns);

                            println!("\nSuggestion: A better device is available!");
                            println!("   Current: {} ({})", device_ip, current_ms);
                            println!("   Better:  {} ({})", better_device.public_ip, better_ms);
                            println!(
                                "   Consider reconnecting to {} for improved performance.",
                                better_device.code
                            );
                        }
                    }
                }
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::servicecontroller::{DoubleZeroStatus, MockServiceController, StatusResponse};
    use doublezero_cli::tests::utils::create_test_client;
    use doublezero_config::Environment;
    use doublezero_sdk::commands::device::list::ListDeviceCommand;
    use doublezero_serviceability::state::{
        accounttype::AccountType,
        device::{Device, DeviceStatus, DeviceType},
    };
    use solana_sdk::pubkey::Pubkey;
    use std::{
        collections::HashMap,
        sync::{Arc, Mutex},
    };

    struct TestFixture {
        pub client: doublezero_cli::doublezerocommand::MockCliCommand,
        pub controller: MockServiceController,
        pub devices: Arc<Mutex<HashMap<Pubkey, Device>>>,
        pub latencies: Arc<Mutex<Vec<LatencyRecord>>>,
        pub status: Arc<Mutex<Vec<StatusResponse>>>,
    }

    impl TestFixture {
        pub fn new() -> Self {
            let mut fixture = Self {
                client: create_test_client(),
                controller: MockServiceController::new(),
                devices: Arc::new(Mutex::new(HashMap::new())),
                latencies: Arc::new(Mutex::new(vec![])),
                status: Arc::new(Mutex::new(vec![])),
            };

            // Setup common mocks
            fixture
                .controller
                .expect_service_controller_check()
                .return_const(true);

            fixture
                .controller
                .expect_service_controller_can_open()
                .return_const(true);

            fixture
                .controller
                .expect_get_env()
                .returning(|| Ok(Environment::default()));

            fixture
                .client
                .expect_get_environment()
                .returning(Environment::default);

            let status = fixture.status.clone();
            fixture
                .controller
                .expect_status()
                .returning(move || Ok(status.lock().unwrap().clone()));

            let latencies = fixture.latencies.clone();
            fixture
                .controller
                .expect_latency()
                .returning(move || Ok(latencies.lock().unwrap().clone()));

            let devices = fixture.devices.clone();
            fixture
                .client
                .expect_list_device()
                .with(mockall::predicate::eq(ListDeviceCommand))
                .returning(move |_| Ok(devices.lock().unwrap().clone()));

            fixture
        }

        pub fn add_device(
            &mut self,
            code: &str,
            ip: &str,
            latency_ns: i32,
            reachable: bool,
            users: u16,
            max_users: u16,
        ) -> Pubkey {
            let pk = Pubkey::new_unique();
            let ip_parsed: std::net::Ipv4Addr = ip.parse().unwrap();

            // Add device
            let device = Device {
                account_type: AccountType::Device,
                index: self.devices.lock().unwrap().len() as u128 + 1,
                bump_seed: 255,
                reference_count: 0,
                code: code.to_string(),
                contributor_pk: Pubkey::default(),
                location_pk: Pubkey::default(),
                exchange_pk: Pubkey::default(),
                device_type: DeviceType::Switch,
                public_ip: ip_parsed.octets().into(),
                dz_prefixes: "10.0.0.0/24".parse().unwrap(),
                status: DeviceStatus::Activated,
                metrics_publisher_pk: Pubkey::default(),
                owner: Pubkey::default(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                max_users,
                users_count: users,
            };

            self.devices.lock().unwrap().insert(pk, device);

            // Add latency
            self.latencies.lock().unwrap().push(LatencyRecord {
                device_pk: pk.to_string(),
                device_code: code.to_string(),
                device_ip: ip.to_string(),
                min_latency_ns: latency_ns,
                max_latency_ns: latency_ns,
                avg_latency_ns: latency_ns,
                reachable,
            });

            pk
        }

        pub fn set_connected_to(&mut self, device_ip: &str) {
            let status = StatusResponse {
                doublezero_status: DoubleZeroStatus {
                    session_status: "connected".to_string(),
                    last_session_update: Some(1234567890),
                },
                tunnel_name: Some("ipip0".to_string()),
                tunnel_src: Some("192.168.1.10".to_string()),
                tunnel_dst: Some(device_ip.to_string()),
                doublezero_ip: Some("10.0.0.5".to_string()),
                user_type: Some("ibrl".to_string()),
            };

            self.status.lock().unwrap().push(status);
        }
    }

    #[tokio::test]
    async fn test_status_suggests_lower_latency_device() {
        let mut fixture = TestFixture::new();

        fixture.add_device("NYC-01", "203.0.113.5", 50_000_000, true, 180, 200);
        fixture.add_device("NYC-02", "203.0.113.10", 30_000_000, true, 175, 200);
        fixture.set_connected_to("203.0.113.5");

        let cmd = StatusCliCommand { json: false };
        let result = cmd
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;

        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_status_suggests_less_loaded_device() {
        let mut fixture = TestFixture::new();

        fixture.add_device("LA-01", "198.51.100.5", 40_000_000, true, 180, 200);
        fixture.add_device("LA-02", "198.51.100.10", 45_000_000, true, 100, 200);
        fixture.set_connected_to("198.51.100.5");

        let cmd = StatusCliCommand { json: false };
        let result = cmd
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;

        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_status_no_suggestion_when_current_is_best() {
        let mut fixture = TestFixture::new();

        fixture.add_device("SF-01", "192.0.2.5", 20_000_000, true, 100, 200);
        fixture.add_device("SF-02", "192.0.2.10", 60_000_000, true, 90, 200);
        fixture.set_connected_to("192.0.2.5");

        let cmd = StatusCliCommand { json: false };
        let result = cmd
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;

        assert!(result.is_ok());
    }
}
