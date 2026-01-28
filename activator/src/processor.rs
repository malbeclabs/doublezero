use crate::{
    idallocator::IDAllocator,
    ipblockallocator::IPBlockAllocator,
    process::{
        accesspass::process_access_pass_event, device::process_device_event,
        exchange::process_exchange_event, link::process_link_event,
        location::process_location_event, multicastgroup::process_multicastgroup_event,
        user::process_user_event,
    },
    states::devicestate::DeviceState,
};
use backon::{BlockingRetryable, ExponentialBuilder};
use doublezero_sdk::{
    commands::{
        device::list::ListDeviceCommand, exchange::list::ListExchangeCommand,
        link::list::ListLinkCommand, location::list::ListLocationCommand,
        user::list::ListUserCommand,
    },
    doublezeroclient::DoubleZeroClient,
    AccountData, DeviceStatus, Exchange, GetGlobalConfigCommand, InterfaceType, LinkStatus,
    Location, MulticastGroup, UserStatus,
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
pub type LocationMap = HashMap<Pubkey, Location>;
pub type ExchangeMap = HashMap<Pubkey, Exchange>;
pub type MulticastGroupMap = HashMap<Pubkey, MulticastGroup>;

pub struct Processor<T: DoubleZeroClient> {
    rx: mpsc::Receiver<(Box<Pubkey>, Box<AccountData>)>,
    client: Arc<T>,
    link_ids: IDAllocator,
    segment_routing_ids: IDAllocator,
    link_ips: IPBlockAllocator,
    multicastgroup_tunnel_ips: IPBlockAllocator,
    user_tunnel_ips: IPBlockAllocator,
    devices: DeviceMap,
    locations: LocationMap,
    exchanges: ExchangeMap,
    multicastgroups: MulticastGroupMap,
    use_onchain_allocation: bool,
}

impl<T: DoubleZeroClient> Processor<T> {
    pub fn new(
        rx: mpsc::Receiver<(Box<Pubkey>, Box<AccountData>)>,
        client: Arc<T>,
        use_onchain_allocation: bool,
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

        for (_, link) in links
            .iter()
            .filter(|(_, l)| l.status == LinkStatus::Activated)
        {
            link_ids.assign(link.tunnel_id);
            link_ips.assign_block(link.tunnel_net.into());
        }

        for (pubkey, device) in devices
            .iter()
            .filter(|(_, d)| d.status == DeviceStatus::Activated)
        {
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

        users
            .iter()
            .filter(|(_, u)| u.status == UserStatus::Activated)
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
                }
                Ok::<(), eyre::Error>(())
            })?;

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
            devices: device_map,
            locations,
            exchanges,
            multicastgroups: HashMap::new(),
            use_onchain_allocation,
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
                    self.use_onchain_allocation,
                );
            }
            AccountData::Link(link) => {
                process_link_event(
                    self.client.as_ref(),
                    pubkey,
                    &mut self.link_ips,
                    &mut self.link_ids,
                    link,
                    self.use_onchain_allocation,
                );
            }
            AccountData::User(user) => {
                process_user_event(
                    self.client.as_ref(),
                    pubkey,
                    &mut self.devices,
                    &mut self.user_tunnel_ips,
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
                    self.use_onchain_allocation,
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
