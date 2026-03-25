use crate::{
    idallocator::IDAllocator,
    ipblockallocator::IPBlockAllocator,
    process::{
        accesspass::process_access_pass_event,
        device::{process_device_event, process_device_event_stateless},
        exchange::process_exchange_event,
        link::{process_link_event, process_link_event_stateless},
        location::process_location_event,
        multicastgroup::{process_multicastgroup_event, process_multicastgroup_event_stateless},
        user::{process_user_event, process_user_event_stateless},
    },
    states::devicestate::{DeviceState, DeviceStateStateless},
};
use backon::{BlockingRetryable, ExponentialBuilder};
use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::{
    commands::{
        device::list::ListDeviceCommand, exchange::list::ListExchangeCommand,
        link::list::ListLinkCommand, location::list::ListLocationCommand,
        user::list::ListUserCommand,
    },
    doublezeroclient::DoubleZeroClient,
    AccountData, Device, DeviceStatus, Exchange, GetGlobalConfigCommand, InterfaceType, Link,
    LinkStatus, Location, MulticastGroup, User, UserStatus, UserType,
};
use log::{debug, error, info, warn};
use solana_sdk::pubkey::Pubkey;
use std::{
    collections::HashMap,
    sync::{atomic::AtomicBool, Arc},
    time::Duration,
};
use tokio::sync::mpsc;

pub type DeviceMap = HashMap<Pubkey, DeviceState>;
pub type DeviceMapStateless = HashMap<Pubkey, DeviceStateStateless>;
pub type LocationMap = HashMap<Pubkey, Location>;
pub type ExchangeMap = HashMap<Pubkey, Exchange>;
pub type MulticastGroupMap = HashMap<Pubkey, MulticastGroup>;

/// Stateful processor for offchain allocation mode.
/// Maintains local allocators for tunnel IDs, IPs, etc.
pub struct Processor<T: DoubleZeroClient> {
    rx: mpsc::Receiver<(Box<Pubkey>, Box<AccountData>)>,
    client: Arc<T>,
    link_ids: IDAllocator,
    segment_routing_ids: IDAllocator,
    link_ips: IPBlockAllocator,
    multicastgroup_tunnel_ips: IPBlockAllocator,
    user_tunnel_ips: IPBlockAllocator,
    publisher_dz_ips: IPBlockAllocator,
    devices: DeviceMap,
    locations: LocationMap,
    exchanges: ExchangeMap,
    multicastgroups: MulticastGroupMap,
}

/// Stateless processor for onchain allocation mode.
/// Does not maintain local allocators - the blockchain handles all allocation.
pub struct ProcessorStateless<T: DoubleZeroClient> {
    rx: mpsc::Receiver<(Box<Pubkey>, Box<AccountData>)>,
    client: Arc<T>,
    devices: DeviceMapStateless,
    multicastgroups: MulticastGroupMap,
}

/// Reserve segment routing IDs and loopback IPs for devices that have active allocations.
/// Devices in Activated, Drained, DeviceProvisioning, or LinkProvisioning states all
/// hold allocated addresses that must not be handed out to new devices.
fn reserve_device_allocations(
    devices: &HashMap<Pubkey, Device>,
    segment_routing_ids: &mut IDAllocator,
    link_ips: &mut IPBlockAllocator,
    device_map: &mut DeviceMap,
) {
    for (pubkey, device) in devices.iter().filter(|(_, d)| {
        matches!(
            d.status,
            DeviceStatus::Activated
                | DeviceStatus::Drained
                | DeviceStatus::DeviceProvisioning
                | DeviceStatus::LinkProvisioning
        )
    }) {
        device.interfaces.iter().for_each(|interface| {
            let interface = interface.into_current_version();
            if interface.node_segment_idx > 0 {
                segment_routing_ids.assign(interface.node_segment_idx);
            }
            if interface.interface_type == InterfaceType::Loopback {
                link_ips.assign_block(interface.ip_net.into());
            }
        });
        device_map
            .entry(*pubkey)
            .or_insert_with(|| DeviceState::new(device));
    }
}

/// Reserve tunnel IPs, tunnel IDs, dz_ips, and publisher IPs for users that
/// have active allocations. Users in Activated, Updating, or OutOfCredits
/// states all hold allocated addresses that must not be handed out to new users.
fn reserve_user_allocations(
    users: &HashMap<Pubkey, User>,
    device_map: &mut DeviceMap,
    user_tunnel_ips: &mut IPBlockAllocator,
    publisher_dz_ips: &mut IPBlockAllocator,
) -> eyre::Result<()> {
    users
        .iter()
        .filter(|(_, u)| {
            matches!(
                u.status,
                UserStatus::Activated | UserStatus::Updating | UserStatus::OutOfCredits
            )
        })
        .try_for_each(|(_, user)| {
            if let Some(device_state) = device_map.get_mut(&user.device_pk) {
                device_state
                    .register(user.dz_ip, user.tunnel_id)
                    .map_err(|e| {
                        eyre::eyre!(
                            "Error registering user dz_ip={} tunnel_id={}: {}",
                            user.dz_ip,
                            user.tunnel_id,
                            e
                        )
                    })?;
                user_tunnel_ips.assign_block(user.tunnel_net.into());

                // Mark publisher IPs as allocated in the publisher pool
                if user.user_type == UserType::Multicast
                    && !user.publishers.is_empty()
                    && user.dz_ip != std::net::Ipv4Addr::UNSPECIFIED
                    && user.dz_ip != user.client_ip
                {
                    if let Ok(dz_ip_net) = NetworkV4::new(user.dz_ip, 32) {
                        publisher_dz_ips.assign_block(dz_ip_net.into());
                        info!(
                            "Marked publisher dz_ip {} as allocated (loaded from existing user)",
                            user.dz_ip
                        );
                    }
                }
                // Register tunnel endpoint if set
                if user.has_tunnel_endpoint() {
                    device_state.register_tunnel_endpoint(user.client_ip, user.tunnel_endpoint);
                }
            }
            Ok::<(), eyre::Error>(())
        })
}

/// Reserve tunnel IDs and IP blocks for links that have active allocations.
/// Links in Activated, HardDrained, SoftDrained, or Provisioning states all
/// hold allocated addresses that must not be handed out to new links.
fn reserve_link_allocations(
    links: &HashMap<Pubkey, Link>,
    link_ids: &mut IDAllocator,
    link_ips: &mut IPBlockAllocator,
) {
    for (_, link) in links.iter().filter(|(_, l)| {
        matches!(
            l.status,
            LinkStatus::Activated
                | LinkStatus::HardDrained
                | LinkStatus::SoftDrained
                | LinkStatus::Provisioning
        )
    }) {
        link_ids.assign(link.tunnel_id);
        link_ips.assign_block(link.tunnel_net.into());
    }
}

impl<T: DoubleZeroClient> Processor<T> {
    pub fn new(
        rx: mpsc::Receiver<(Box<Pubkey>, Box<AccountData>)>,
        client: Arc<T>,
    ) -> eyre::Result<Self> {
        let builder = ExponentialBuilder::new()
            .with_max_times(5)
            .with_min_delay(Duration::from_secs(1));

        let get_config = || GetGlobalConfigCommand.execute(client.as_ref());

        // Wait for the global config to be available
        // This is a workaround for the fact that the global config is not available immediately
        let (_, config) = get_config
            .retry(builder)
            .notify(|_, _| warn!("Waiting for config..."))
            .call()
            .expect("Failed to get global config after retries");

        let unspecified = NetworkV4::default();
        if config.device_tunnel_block == unspecified {
            return Err(eyre::eyre!(
                "Global config device_tunnel_block is not set (0.0.0.0/0)"
            ));
        }
        if config.user_tunnel_block == unspecified {
            return Err(eyre::eyre!(
                "Global config user_tunnel_block is not set (0.0.0.0/0)"
            ));
        }
        if config.multicastgroup_block == unspecified {
            return Err(eyre::eyre!(
                "Global config multicastgroup_block is not set (0.0.0.0/0)"
            ));
        }
        if config.multicast_publisher_block == unspecified {
            return Err(eyre::eyre!(
                "Global config multicast_publisher_block is not set (0.0.0.0/0)"
            ));
        }

        let devices = ListDeviceCommand.execute(client.as_ref())?;
        let links = ListLinkCommand.execute(client.as_ref())?;
        let users = ListUserCommand.execute(client.as_ref())?;
        let locations = ListLocationCommand.execute(client.as_ref())?;
        let exchanges = ListExchangeCommand.execute(client.as_ref())?;
        let mut device_map: DeviceMap = DeviceMap::new();
        let mut link_ids = IDAllocator::new(0, vec![]);
        let mut link_ips = IPBlockAllocator::new(config.device_tunnel_block.into());
        let mut segment_routing_ids = IDAllocator::new(1, vec![]);
        let mut user_tunnel_ips = IPBlockAllocator::new(config.user_tunnel_block.into());
        let mut publisher_dz_ips = IPBlockAllocator::new(config.multicast_publisher_block.into());

        reserve_link_allocations(&links, &mut link_ids, &mut link_ips);

        reserve_device_allocations(
            &devices,
            &mut segment_routing_ids,
            &mut link_ips,
            &mut device_map,
        );

        reserve_user_allocations(
            &users,
            &mut device_map,
            &mut user_tunnel_ips,
            &mut publisher_dz_ips,
        )?;

        info!(
            "Number of - devices: {} links: {} users: {}",
            devices.len(),
            links.len(),
            users.len(),
        );

        Ok(Self {
            rx,
            client,
            link_ips,
            link_ids,
            segment_routing_ids,
            multicastgroup_tunnel_ips: IPBlockAllocator::new(config.multicastgroup_block.into()),
            user_tunnel_ips,
            publisher_dz_ips,
            devices: device_map,
            locations,
            exchanges,
            multicastgroups: HashMap::new(),
        })
    }

    pub async fn run(&mut self, stop_signal: Arc<AtomicBool>) {
        info!("Processor running...");
        while !stop_signal.load(std::sync::atomic::Ordering::Relaxed) {
            if let Some((pubkey, data)) = self.rx.recv().await {
                self.process_event(&pubkey, &data);
            }
        }
        info!("Processor done");
    }

    fn process_event(&mut self, pubkey: &Pubkey, data: &AccountData) {
        debug!("Event: {pubkey} {data:?}");

        match data {
            AccountData::Device(device) => {
                process_device_event(
                    self.client.as_ref(),
                    pubkey,
                    &mut self.devices,
                    device,
                    &mut self.segment_routing_ids,
                    &mut self.link_ips,
                );
            }
            AccountData::Link(link) => {
                process_link_event(
                    self.client.as_ref(),
                    pubkey,
                    &mut self.link_ips,
                    &mut self.link_ids,
                    link,
                );
            }
            AccountData::User(user) => {
                process_user_event(
                    self.client.as_ref(),
                    pubkey,
                    &mut self.devices,
                    &mut self.user_tunnel_ips,
                    &mut self.publisher_dz_ips,
                    &mut self.link_ids,
                    user,
                    &self.locations,
                    &self.exchanges,
                );
            }
            AccountData::Location(location) => {
                process_location_event(pubkey, &mut self.locations, location);
            }
            AccountData::Exchange(exchange) => {
                process_exchange_event(pubkey, &mut self.exchanges, exchange);
            }
            AccountData::MulticastGroup(multicastgroup) => {
                let _ = process_multicastgroup_event(
                    self.client.as_ref(),
                    pubkey,
                    multicastgroup,
                    &mut self.multicastgroups,
                    &mut self.multicastgroup_tunnel_ips,
                )
                .inspect_err(|e| {
                    error!("Error processing multicast group event: {e}");
                });
            }
            AccountData::AccessPass(access_pass) => {
                let users = ListUserCommand
                    .execute(self.client.as_ref())
                    .unwrap_or_default();
                let _ =
                    process_access_pass_event(self.client.as_ref(), pubkey, access_pass, &users)
                        .inspect_err(|e| {
                            error!("Error processing access pass event: {e}");
                        });
            }
            _ => {}
        };
        metrics::counter!("doublezero_activator_event_handled").increment(1);
    }
}

impl<T: DoubleZeroClient> ProcessorStateless<T> {
    pub fn new(
        rx: mpsc::Receiver<(Box<Pubkey>, Box<AccountData>)>,
        client: Arc<T>,
    ) -> eyre::Result<Self> {
        let builder = ExponentialBuilder::new()
            .with_max_times(5)
            .with_min_delay(Duration::from_secs(1));

        let get_config = || GetGlobalConfigCommand.execute(client.as_ref());

        // Wait for the global config to be available
        let (_, _config) = get_config
            .retry(builder)
            .notify(|_, _| warn!("Waiting for config..."))
            .call()
            .expect("Failed to get global config after retries");

        // In stateless mode, we still cache device/location/exchange info for logging/context,
        // but we don't track allocation state
        let devices = ListDeviceCommand.execute(client.as_ref())?;

        let mut device_map: DeviceMapStateless = DeviceMapStateless::new();

        // Only cache device info, no allocation tracking
        for (pubkey, device) in devices
            .iter()
            .filter(|(_, d)| d.status == DeviceStatus::Activated)
        {
            device_map
                .entry(*pubkey)
                .or_insert_with(|| DeviceStateStateless::new(device));
        }

        info!("Number of - devices: {} (stateless mode)", devices.len(),);

        Ok(Self {
            rx,
            client,
            devices: device_map,
            multicastgroups: HashMap::new(),
        })
    }

    pub async fn run(&mut self, stop_signal: Arc<AtomicBool>) {
        info!("ProcessorStateless running (onchain allocation mode)...");
        while !stop_signal.load(std::sync::atomic::Ordering::Relaxed) {
            if let Some((pubkey, data)) = self.rx.recv().await {
                self.process_event(&pubkey, &data);
            }
        }
        info!("ProcessorStateless done");
    }

    fn process_event(&mut self, pubkey: &Pubkey, data: &AccountData) {
        debug!("Event: {pubkey} {data:?}");

        match data {
            AccountData::Device(device) => {
                process_device_event_stateless(
                    self.client.as_ref(),
                    pubkey,
                    &mut self.devices,
                    device,
                );
            }
            AccountData::Link(link) => {
                process_link_event_stateless(self.client.as_ref(), pubkey, link);
            }
            AccountData::User(user) => {
                process_user_event_stateless(self.client.as_ref(), pubkey, &mut self.devices, user);
            }
            AccountData::MulticastGroup(multicastgroup) => {
                let _ = process_multicastgroup_event_stateless(
                    self.client.as_ref(),
                    pubkey,
                    multicastgroup,
                    &mut self.multicastgroups,
                )
                .inspect_err(|e| {
                    error!("Error processing multicast group event: {e}");
                });
            }
            AccountData::AccessPass(access_pass) => {
                let users = ListUserCommand
                    .execute(self.client.as_ref())
                    .unwrap_or_default();
                let _ =
                    process_access_pass_event(self.client.as_ref(), pubkey, access_pass, &users)
                        .inspect_err(|e| {
                            error!("Error processing access pass event: {e}");
                        });
            }
            _ => {}
        };
        metrics::counter!("doublezero_activator_event_handled").increment(1);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_program_common::types::NetworkV4;
    use doublezero_sdk::{
        AccountType, GlobalConfig, Link, LinkLinkType, LinkStatus, MockDoubleZeroClient,
    };
    use doublezero_serviceability::pda::get_globalconfig_pda;
    use mockall::predicate;
    use std::{collections::HashMap, sync::Arc};
    use tokio::sync::mpsc;

    fn valid_config() -> GlobalConfig {
        GlobalConfig {
            account_type: AccountType::GlobalConfig,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            local_asn: 123,
            remote_asn: 456,
            device_tunnel_block: "1.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "2.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "239.239.239.0/24".parse().unwrap(),
            multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
            next_bgp_community: 0,
        }
    }

    fn mock_client_with_config(config: GlobalConfig) -> Arc<MockDoubleZeroClient> {
        let mut client = MockDoubleZeroClient::new();
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);
        let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);
        client
            .expect_get()
            .with(predicate::eq(globalconfig_pubkey))
            .returning(move |_| Ok(AccountData::GlobalConfig(config.clone())));
        client.expect_gets().returning(move |_| Ok(HashMap::new()));
        Arc::new(client)
    }

    #[tokio::test]
    async fn test_processor_new_rejects_unset_device_tunnel_block() {
        let mut config = valid_config();
        config.device_tunnel_block = NetworkV4::default();
        let client = mock_client_with_config(config);
        let (_tx, rx) = mpsc::channel(1);
        let result = Processor::new(rx, client);
        let err = result.err().expect("expected error").to_string();
        assert!(err.contains("device_tunnel_block"), "error was: {err}");
    }

    #[tokio::test]
    async fn test_processor_new_rejects_unset_user_tunnel_block() {
        let mut config = valid_config();
        config.user_tunnel_block = NetworkV4::default();
        let client = mock_client_with_config(config);
        let (_tx, rx) = mpsc::channel(1);
        let result = Processor::new(rx, client);
        let err = result.err().expect("expected error").to_string();
        assert!(err.contains("user_tunnel_block"), "error was: {err}");
    }

    #[tokio::test]
    async fn test_processor_new_rejects_unset_multicastgroup_block() {
        let mut config = valid_config();
        config.multicastgroup_block = NetworkV4::default();
        let client = mock_client_with_config(config);
        let (_tx, rx) = mpsc::channel(1);
        let result = Processor::new(rx, client);
        let err = result.err().expect("expected error").to_string();
        assert!(err.contains("multicastgroup_block"), "error was: {err}");
    }

    #[tokio::test]
    async fn test_processor_new_rejects_unset_multicast_publisher_block() {
        let mut config = valid_config();
        config.multicast_publisher_block = NetworkV4::default();
        let client = mock_client_with_config(config);
        let (_tx, rx) = mpsc::channel(1);
        let result = Processor::new(rx, client);
        let err = result.err().expect("expected error").to_string();
        assert!(
            err.contains("multicast_publisher_block"),
            "error was: {err}"
        );
    }

    /// Regression test: reserve_user_allocations must reserve tunnel_net for users
    /// in Updating and OutOfCredits states. Otherwise new users get colliding addresses.
    #[test]
    fn test_updating_user_allocations_must_be_reserved_at_startup() {
        use crate::{ipblockallocator::IPBlockAllocator, states::devicestate::DeviceState};
        use doublezero_sdk::{
            AccountType, Device, DeviceStatus, DeviceType, User, UserStatus, UserType,
        };
        use doublezero_serviceability::state::user::UserCYOA;
        use std::net::Ipv4Addr;

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            reference_count: 0,
            bump_seed: 0,
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Hybrid,
            public_ip: [192, 168, 1, 2].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
        };

        let mut device_map: DeviceMap = DeviceMap::new();
        device_map.insert(device_pubkey, DeviceState::new(&device));

        // Existing user in Updating state with tunnel_net 2.0.0.0/31
        let updating_user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            user_type: UserType::IBRL,
            tenant_pk: Pubkey::new_unique(),
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1].into(),
            dz_ip: [10, 0, 0, 1].into(),
            tunnel_id: 500,
            tunnel_net: "2.0.0.0/31".parse().unwrap(),
            status: UserStatus::Updating,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
        };

        let mut users: HashMap<Pubkey, User> = HashMap::new();
        users.insert(Pubkey::new_unique(), updating_user);

        let mut user_tunnel_ips = IPBlockAllocator::new("2.0.0.0/24".parse().unwrap());
        let mut publisher_dz_ips = IPBlockAllocator::new("148.51.120.0/21".parse().unwrap());

        reserve_user_allocations(
            &users,
            &mut device_map,
            &mut user_tunnel_ips,
            &mut publisher_dz_ips,
        )
        .expect("reserve_user_allocations should succeed");

        // The next allocation should NOT collide with the Updating user's 2.0.0.0/31
        let next_block = user_tunnel_ips
            .next_available_block(0, 2)
            .expect("should have available block");
        assert_ne!(
            next_block.ip().to_string(),
            "2.0.0.0",
            "BUG: allocated tunnel_net collides with Updating user's 2.0.0.0/31"
        );
    }

    /// Regression test: reserve_device_allocations must reserve loopback IPs and segment
    /// routing IDs for devices in Drained/DeviceProvisioning/LinkProvisioning states.
    /// Otherwise new devices get colliding addresses.
    #[test]
    fn test_drained_device_allocations_must_be_reserved_at_startup() {
        use crate::{idallocator::IDAllocator, ipblockallocator::IPBlockAllocator};
        use doublezero_sdk::{
            AccountType, CurrentInterfaceVersion, Device, DeviceStatus, DeviceType,
            InterfaceStatus, InterfaceType, LoopbackType,
        };

        // Existing device in Drained state with loopback IP 1.0.0.0/32 and segment routing ID 1
        let drained_device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Hybrid,
            public_ip: [192, 168, 1, 1].into(),
            status: DeviceStatus::Drained,
            metrics_publisher_pk: Pubkey::default(),
            code: "DrainedDevice".to_string(),
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![CurrentInterfaceVersion {
                status: InterfaceStatus::Activated,
                name: "Loopback0".to_string(),
                interface_type: InterfaceType::Loopback,
                loopback_type: LoopbackType::Vpnv4,
                vlan_id: 0,
                ip_net: "1.0.0.0/32".parse().unwrap(),
                node_segment_idx: 1,
                user_tunnel_endpoint: false,
                ..Default::default()
            }
            .to_interface()],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
        };

        let mut devices: HashMap<Pubkey, Device> = HashMap::new();
        devices.insert(Pubkey::new_unique(), drained_device);

        let mut segment_routing_ids = IDAllocator::new(1, vec![]);
        let mut link_ips = IPBlockAllocator::new("1.0.0.0/24".parse().unwrap());
        let mut device_map: DeviceMap = DeviceMap::new();

        reserve_device_allocations(
            &devices,
            &mut segment_routing_ids,
            &mut link_ips,
            &mut device_map,
        );

        // The next segment routing ID should NOT collide with the Drained device's ID 1
        let next_sr_id = segment_routing_ids.next_available();
        assert_ne!(
            next_sr_id, 1,
            "BUG: allocated segment routing ID 1 collides with Drained device"
        );

        // The next loopback IP should NOT collide with the Drained device's 1.0.0.0/32
        let next_ip = link_ips
            .next_available_block(1, 1)
            .expect("should have available block");
        assert_ne!(
            next_ip.ip().to_string(),
            "1.0.0.0",
            "BUG: allocated loopback IP collides with Drained device's 1.0.0.0/32"
        );

        // The device should be in the device_map
        assert_eq!(
            device_map.len(),
            1,
            "Drained device should be in device_map"
        );
    }

    /// Regression test: Processor::new must reserve tunnel_net/tunnel_id for links in
    /// HardDrained/SoftDrained/Provisioning states. Otherwise new links get colliding addresses.
    ///
    /// This test simulates the Processor::new initialization logic: it iterates over
    /// existing links and only reserves addresses for those matching the filter.
    /// Then it calls process_link_event with a new Pending link and asserts the
    /// allocated tunnel_net does NOT collide with the existing link.
    #[test]
    fn test_drained_link_tunnel_net_must_be_reserved_at_startup() {
        use crate::{
            idallocator::IDAllocator,
            ipblockallocator::IPBlockAllocator,
            process::link::process_link_event,
            tests::utils::{create_test_client, get_tunnel_bump_seed},
        };
        use doublezero_serviceability::instructions::DoubleZeroInstruction;
        use solana_sdk::signature::Signature;

        let mut client = create_test_client();

        // Existing link in HardDrained state — has tunnel_net 1.0.0.0/31 and tunnel_id 5
        let drained_link = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_tunnel_bump_seed(&client),
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 1500,
            delay_ns: 20_000,
            jitter_ns: 100,
            delay_override_ns: 0,
            tunnel_id: 5,
            tunnel_net: "1.0.0.0/31".parse().unwrap(),
            status: LinkStatus::HardDrained,
            code: "DrainedLink".to_string(),
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: "Ethernet1".to_string(),
            link_health: doublezero_serviceability::state::link::LinkHealth::Pending,
            desired_status: doublezero_serviceability::state::link::LinkDesiredStatus::Activated,
        };

        let mut existing_links: HashMap<Pubkey, Link> = HashMap::new();
        existing_links.insert(Pubkey::new_unique(), drained_link);
        let mut link_ips = IPBlockAllocator::new("1.0.0.0/24".parse().unwrap());
        let mut link_ids = IDAllocator::new(0, vec![]);

        // Uses the same function as Processor::new
        reserve_link_allocations(&existing_links, &mut link_ids, &mut link_ips);

        // New pending link arrives — the allocator should give it a non-colliding address
        let new_link_pubkey = Pubkey::new_unique();
        let new_link = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: get_tunnel_bump_seed(&client),
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 1500,
            delay_ns: 20_000,
            jitter_ns: 100,
            delay_override_ns: 0,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: LinkStatus::Pending,
            code: "NewLink".to_string(),
            side_a_iface_name: "Ethernet2".to_string(),
            side_z_iface_name: "Ethernet3".to_string(),
            link_health: doublezero_serviceability::state::link::LinkHealth::Pending,
            desired_status: doublezero_serviceability::state::link::LinkDesiredStatus::Activated,
        };

        let new_link_cloned = new_link.clone();
        client
            .expect_get()
            .with(predicate::eq(new_link_pubkey))
            .times(1)
            .returning(move |_| Ok(AccountData::Link(new_link_cloned.clone())));

        // Assert the allocated tunnel_net does NOT collide with the drained link's 1.0.0.0/31
        client
            .expect_execute_transaction()
            .withf(|instruction, _| {
                if let DoubleZeroInstruction::ActivateLink(args) = instruction {
                    let allocated_net: String = args.tunnel_net.to_string();
                    assert_ne!(
                        allocated_net, "1.0.0.0/31",
                        "BUG: allocated tunnel_net 1.0.0.0/31 collides with HardDrained link"
                    );
                    assert_ne!(
                        args.tunnel_id, 5,
                        "BUG: allocated tunnel_id 5 collides with HardDrained link"
                    );
                    true
                } else {
                    false
                }
            })
            .times(1)
            .returning(|_, _| Ok(Signature::new_unique()));

        process_link_event(
            &client,
            &new_link_pubkey,
            &mut link_ips,
            &mut link_ids,
            &new_link,
        );
    }
}
