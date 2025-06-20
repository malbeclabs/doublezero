use crate::{
    activator_metrics::ActivatorMetrics,
    idallocator::IDAllocator,
    ipblockallocator::IPBlockAllocator,
    metrics_service::MetricsService,
    process::{
        device::process_device_event, exchange::process_exchange_event, link::process_tunnel_event,
        location::process_location_event, multicastgroup::process_multicastgroup_event,
        user::process_user_event,
    },
    states::devicestate::DeviceState,
};
use doublezero_cli::{checkversion::check_version, doublezerocommand::CliCommandImpl};
use doublezero_sdk::{
    commands::{
        device::list::ListDeviceCommand, exchange::list::ListExchangeCommand,
        link::list::ListLinkCommand, location::list::ListLocationCommand,
        user::list::ListUserCommand,
    },
    AccountData, DZClient, Device, DeviceStatus, Exchange, GetGlobalConfigCommand, LinkStatus,
    Location, MulticastGroup, ProgramVersion, UserStatus,
};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, thread, time::Duration};

pub type DeviceMap = HashMap<Pubkey, DeviceState>;

pub struct Activator {
    pub client: DZClient,

    pub tunnel_tunnel_ids: IDAllocator,
    pub tunnel_tunnel_ips: IPBlockAllocator,
    pub multicastgroup_tunnel_ips: IPBlockAllocator,

    pub user_tunnel_ips: IPBlockAllocator,
    pub devices: DeviceMap,

    locations: HashMap<Pubkey, Location>,
    exchanges: HashMap<Pubkey, Exchange>,
    multicastgroups: HashMap<Pubkey, MulticastGroup>,
    metrics: ActivatorMetrics,
    state_transitions: HashMap<&'static str, usize>,
}

impl Activator {
    /// Creates a new Activator instance.
    /// Initializes the IPBlockAllocator for tunnels, users, and devices.
    pub async fn new(
        rpc_url: Option<String>,
        websocket_url: Option<String>,
        program_id: Option<String>,
        kaypair: Option<String>,
        metrics_service: Box<dyn MetricsService + Send + Sync>,
    ) -> eyre::Result<Self> {
        let client = DZClient::new(rpc_url, websocket_url, program_id, kaypair)?;

        print!(
            "Connected to url: {} ws: {} program_id: {} ",
            client.get_rpc(),
            client.get_ws(),
            client.get_program_id()
        );

        let stdout = std::io::stdout();
        let mut handle = stdout.lock();

        // Check the version of the client against the program version
        let cli = CliCommandImpl::new(&client);
        check_version(&cli, &mut handle, ProgramVersion::current())?;

        // Wait for the global config to be available
        // This is a workaround for the fact that the global config is not available immediately
        let (_, config) = loop {
            match GetGlobalConfigCommand.execute(&client) {
                Ok(result) => break result,
                Err(_) => {
                    println!("Waiting for config...");
                    thread::sleep(Duration::from_secs(10));
                }
            }
        };

        Ok(Self {
            client,
            tunnel_tunnel_ips: IPBlockAllocator::new(config.device_tunnel_block.into()),
            tunnel_tunnel_ids: IDAllocator::new(0, vec![]),
            multicastgroup_tunnel_ips: IPBlockAllocator::new(config.multicastgroup_block.into()),
            user_tunnel_ips: IPBlockAllocator::new(config.user_tunnel_block.into()),
            devices: HashMap::new(),
            metrics: ActivatorMetrics::new(metrics_service),
            locations: HashMap::new(),
            exchanges: HashMap::new(),
            multicastgroups: HashMap::new(),
            state_transitions: HashMap::new(),
        })
    }

    pub async fn init(&mut self) -> eyre::Result<()> {
        // Fetch the list of tunnels, devices, and users from the client
        let devices = ListDeviceCommand.execute(&self.client)?;
        let tunnels = ListLinkCommand.execute(&self.client)?;
        let users = ListUserCommand.execute(&self.client)?;
        self.locations = ListLocationCommand.execute(&self.client)?;
        self.exchanges = ListExchangeCommand.execute(&self.client)?;

        for (_, tunnel) in tunnels
            .iter()
            .filter(|(_, t)| t.status == LinkStatus::Activated)
        {
            self.tunnel_tunnel_ids.assign(tunnel.tunnel_id);
            self.tunnel_tunnel_ips
                .assign_block(tunnel.tunnel_net.into());
        }

        for (pubkey, device) in devices
            .iter()
            .filter(|(_, d)| d.status == DeviceStatus::Activated)
        {
            self.add_device(pubkey, device);
        }

        users
            .iter()
            .filter(|(_, u)| u.status == UserStatus::Activated)
            .try_for_each(|(_, user)| {
                if let Some(device_state) = self.devices.get_mut(&user.device_pk) {
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
                    self.user_tunnel_ips.assign_block(user.tunnel_net.into());
                }
                Ok::<(), eyre::Error>(())
            })?;

        println!(
            "devices: {} tunnels: {} users: {}",
            devices.len(),
            tunnels.len(),
            users.len(),
        );

        Ok(())
    }

    fn add_device(&mut self, pubkey: &Pubkey, device: &Device) {
        self.devices
            .entry(*pubkey)
            .or_insert_with(|| DeviceState::new(device));
    }

    pub fn run(&mut self) -> eyre::Result<()> {
        self.metrics.record_metrics(
            &self.devices,
            &self.locations,
            &self.exchanges,
            &self.state_transitions,
        )?;

        self.devices.iter().for_each(|(_pubkey, device)| {
            println!(
                "Device code: {} public_ip: {} dz_prefixes: {} tunnels: {} tunnel_net: {} assigned: {}",
                device.device.code,
                device.device.public_ip,
                &device.device.dz_prefixes,
                device.tunnel_ids.display_assigned(),
                self.user_tunnel_ips.base_block,
                self.user_tunnel_ips.display_assigned_ips(),
            );
        });

        // store these so we can move them into the below closure without making the borrow checker mad
        let devices = &mut self.devices;
        let tunnel_tunnel_ips = &mut self.tunnel_tunnel_ips;
        let tunnel_tunnel_ids = &mut self.tunnel_tunnel_ids;
        let multicastgroup_tunnel_ips = &mut self.multicastgroup_tunnel_ips;
        let user_tunnel_ips = &mut self.user_tunnel_ips;
        let metrics = &self.metrics;
        let locations = &mut self.locations;
        let exchanges = &mut self.exchanges;
        let multicastgroups = &mut self.multicastgroups;
        let state_transitions = &mut self.state_transitions;

        self.client
            .gets_and_subscribe(move |client, pubkey, data| {
                match data {
                    AccountData::Device(device) => {
                        process_device_event(client, pubkey, devices, device, state_transitions);
                    }
                    AccountData::Link(tunnel) => {
                        process_tunnel_event(
                            client,
                            pubkey,
                            tunnel_tunnel_ips,
                            tunnel_tunnel_ids,
                            tunnel,
                            state_transitions,
                        );
                    }
                    AccountData::User(user) => {
                        process_user_event(
                            client,
                            pubkey,
                            devices,
                            user_tunnel_ips,
                            tunnel_tunnel_ids,
                            user,
                            state_transitions,
                        );
                    }
                    AccountData::Location(location) => {
                        process_location_event(pubkey, locations, location);
                    }
                    AccountData::Exchange(exchange) => {
                        process_exchange_event(pubkey, exchanges, exchange);
                    }
                    AccountData::MulticastGroup(multicastgroup) => {
                        process_multicastgroup_event(
                            client,
                            pubkey,
                            multicastgroup,
                            multicastgroups,
                            multicastgroup_tunnel_ips,
                            state_transitions,
                        )
                        .unwrap_or_else(|e| {
                            eprintln!("Error processing multicast group event: {e}");
                        });
                    }
                    _ => {}
                };
                if let Err(e) =
                    metrics.record_metrics(devices, locations, exchanges, state_transitions)
                {
                    // Just log the error
                    eprintln!("error on record_metrics: {e}")
                }
            })?;
        Ok(())
    }
}
