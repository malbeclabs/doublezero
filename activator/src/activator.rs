use std::collections::HashMap;

use doublezero_sdk::{
    commands::{
        device::{
            activate::ActivateDeviceCommand, deactivate::DeactivateDeviceCommand,
            get::GetDeviceCommand, list::ListDeviceCommand,
        },
        tunnel::{
            activate::ActivateTunnelCommand, deactivate::DeactivateTunnelCommand,
            list::ListTunnelCommand, reject::RejectTunnelCommand,
        },
        user::{
            activate::ActivateUserCommand, ban::BanUserCommand, deactivate::DeactivateUserCommand,
            list::ListUserCommand, reject::RejectUserCommand,
        },
    },
    ipv4_to_string, networkv4_list_to_string, networkv4_to_string, AccountData, DZClient, Device,
    DeviceStatus, DoubleZeroClient, IpV4, Tunnel, TunnelStatus, User, UserStatus, UserType,
};
use solana_sdk::pubkey::Pubkey;
use std::thread;
use std::time::Duration;

use crate::{
    idallocator::IDAllocator, ipblockallocator::IPBlockAllocator, states::devicestate::DeviceState,
};

pub type DeviceMap = HashMap<Pubkey, DeviceState>;

pub struct Activator {
    pub client: DZClient,

    pub tunnel_tunnel_ids: IDAllocator,
    pub tunnel_tunnel_ips: IPBlockAllocator,

    pub user_tunnel_ips: IPBlockAllocator,
    pub devices: DeviceMap,
}

impl Activator {
    /// Creates a new Activator instance.
    /// Initializes the IPBlockAllocator for tunnels, users, and devices.
    pub async fn new(
        rpc_url: Option<String>,
        websocket_url: Option<String>,
        program_id: Option<String>,
        kaypair: Option<String>,
    ) -> eyre::Result<Self> {
        let client = DZClient::new(rpc_url, websocket_url, program_id, kaypair)?;

        let mut config = client.get_globalconfig();

        while config.is_err() {
            println!("Waiting for config...");
            thread::sleep(Duration::from_secs(10));
            config = client.get_globalconfig();
        }

        let (_, config) = config.unwrap();

        Ok(Self {
            client,
            tunnel_tunnel_ips: IPBlockAllocator::new(config.tunnel_tunnel_block),
            tunnel_tunnel_ids: IDAllocator::new(0, vec![]),
            user_tunnel_ips: IPBlockAllocator::new(config.user_tunnel_block),
            devices: HashMap::new(),
        })
    }

    pub async fn init(&mut self) -> eyre::Result<()> {
        print!(
            "Connected to url: {} ws: {} program_id: {} ",
            self.client.get_rpc(),
            self.client.get_ws(),
            self.client.get_program_id().to_string()
        );

        // Fetch the list of tunnels, devices, and users from the client
        let devices = ListDeviceCommand {}.execute(&self.client)?;
        let tunnels = ListTunnelCommand {}.execute(&self.client)?;
        let users = ListUserCommand {}.execute(&self.client)?;

        for (_, tunnel) in tunnels
            .iter()
            .filter(|(_, t)| t.status == TunnelStatus::Activated)
        {
            self.tunnel_tunnel_ids.assign(tunnel.tunnel_id);
            self.tunnel_tunnel_ips.assign_block(tunnel.tunnel_net);
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
            .for_each(|(_, user)| {
                let device_state = self.devices.get_mut(&user.device_pk).unwrap();
                device_state.register(user.dz_ip, user.tunnel_id);

                self.user_tunnel_ips.assign_block(user.tunnel_net);
            });

        println!(
            "devices: {} tunnels: {} users: {}",
            devices.len().to_string(),
            tunnels.len().to_string(),
            users.len().to_string()
        );

        Ok(())
    }

    fn add_device(&mut self, pubkey: &Pubkey, device: &Device) {
        if !self.devices.contains_key(pubkey) {
            self.devices.insert(*pubkey, DeviceState::new(device));
        }
    }

    pub fn run(&mut self) -> eyre::Result<()> {
        self.devices.iter().for_each(|(_pubkey, device)| {
            print!(
                "Device code: {} public_ip: {} dz_prefixes: {} tunnels: ",
                device.device.code,
                ipv4_to_string(&device.device.public_ip),
                networkv4_list_to_string(&device.device.dz_prefixes)
            );

            if device.tunnel_ids.assigned.len() == 0 {
                print!("{},", "-");
            }
            device.tunnel_ids.assigned.iter().for_each(|tunnel_id| {
                print!("{},", tunnel_id.to_string());
            });
            println!("\x08 ");
        });

        print!(
            "tunnel_net: {} assigned: ",
            self.user_tunnel_ips.base_block.to_string()
        );
        if self.user_tunnel_ips.assigned_ips.len() == 0 {
            print!("{},", "-");
        }
        self.user_tunnel_ips.print_assigned_ips();
        println!("\x08 ");

        self.client
            .gets_and_subscribe(|client, pubkey, data| match data {
                AccountData::Device(device) => {
                    process_device_event(client, pubkey, &mut self.devices, device);
                }
                AccountData::Tunnel(tunnel) => {
                    process_tunnel_event(
                        client,
                        &mut self.tunnel_tunnel_ips,
                        &mut self.tunnel_tunnel_ids,
                        tunnel,
                    );
                }
                AccountData::User(user) => {
                    process_user_event(
                        client,
                        pubkey,
                        &mut self.devices,
                        &mut self.user_tunnel_ips,
                        &mut self.tunnel_tunnel_ids,
                        user,
                    );
                }
                _ => {}
            })?;
        Ok(())
    }
}

fn process_device_event(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    devices: &mut DeviceMap,
    device: &Device,
) {
    match device.status {
        DeviceStatus::Pending => {
            print!("New Device {} ", device.code);

            let res = ActivateDeviceCommand {
                index: device.index,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    println!("Activated {}", signature.to_string());

                    println!(
                        "Add Device: {} public_ip: {} dz_prefixes: {} ",
                        device.code,
                        ipv4_to_string(&device.public_ip),
                        networkv4_list_to_string(&device.dz_prefixes)
                    );
                    devices.insert(*pubkey, DeviceState::new(device));
                }
                Err(e) => println!("Error: {}", e.to_string()),
            }
        }
        DeviceStatus::Activated => {
            if !devices.contains_key(pubkey) {
                println!(
                    "Add Device: {} public_ip: {} dz_prefixes: {} ",
                    device.code,
                    ipv4_to_string(&device.public_ip),
                    networkv4_list_to_string(&device.dz_prefixes)
                );

                devices.insert(*pubkey, DeviceState::new(device));
            } else {
                let device_state = devices.get_mut(pubkey).unwrap();
                device_state.update(device);
            }
        }
        DeviceStatus::Deleting => {
            print!("Deleting Device {} ", device.code);

            let res = DeactivateDeviceCommand {
                index: device.index,
                owner: device.owner,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    println!("Deactivated {}", signature.to_string());
                    devices.remove(pubkey);
                }
                Err(e) => println!("Error: {}", e),
            }
        }
        _ => {}
    }
}

fn process_tunnel_event(
    client: &dyn DoubleZeroClient,
    tunnel_tunnel_ips: &mut IPBlockAllocator,
    tunnel_tunnel_ids: &mut IDAllocator,
    tunnel: &Tunnel,
) {
    match tunnel.status {
        TunnelStatus::Pending => {
            print!("New Tunnel {} ", tunnel.code);

            match tunnel_tunnel_ips.next_available_block(0, 2) {
                Some(tunnel_net) => {
                    let tunnel_id = tunnel_tunnel_ids.next_available();

                    let res = ActivateTunnelCommand {
                        index: tunnel.index,
                        tunnel_id: tunnel_id,
                        tunnel_net: tunnel_net,
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => println!("Activated {}", signature.to_string()),
                        Err(e) => println!("Error: activate_tunnel: {}", e.to_string()),
                    }
                }
                None => {
                    println!("{}", "Error: No available tunnel block");

                    let res = RejectTunnelCommand {
                        index: tunnel.index,
                        reason: "Error: No available tunnel block".to_string(),
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => println!("Rejected {}", signature.to_string()),
                        Err(e) => println!("Error: reject_tunnel: {}", e.to_string()),
                    }
                }
            }
        }
        TunnelStatus::Deleting => {
            print!("Deleting Tunnel {} ", tunnel.code);

            tunnel_tunnel_ids.unassign(tunnel.tunnel_id);
            tunnel_tunnel_ips.unassign_block(tunnel.tunnel_net);

            let res = DeactivateTunnelCommand {
                index: tunnel.index,
                owner: tunnel.owner,
            }
            .execute(client);

            match res {
                Ok(signature) => println!("Deactivated {}", signature.to_string()),
                Err(e) => println!("{}: {}", "Error: deactivate_tunnel:", e.to_string()),
            }
        }
        _ => {}
    }
}

fn process_user_event(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    devices: &mut DeviceMap,
    user_tunnel_ips: &mut IPBlockAllocator,
    tunnel_tunnel_ids: &mut IDAllocator,
    user: &User,
) {
    match user.status {
        // Create User
        UserStatus::Pending => {
            print!("Activating User   {} ", ipv4_to_string(&user.client_ip));
            // Load Device if not exists
            if !devices.contains_key(&user.device_pk) {
                let res = GetDeviceCommand {
                    pubkey_or_code: user.device_pk.to_string(),
                }
                .execute(client);

                match res {
                    Ok((_, device)) => {
                        println!(
                            "Add Device: {} public_ip: {} dz_prefixes: {} ",
                            device.code,
                            ipv4_to_string(&device.public_ip),
                            networkv4_list_to_string(&device.dz_prefixes)
                        );

                        devices.insert(*pubkey, DeviceState::new(&device));
                    }
                    Err(e) => {
                        println!("Error: {}", e.to_string());
                    }
                }
            }

            match devices.get_mut(&user.device_pk) {
                Some(device_state) => {
                    print!("for {} ", device_state.device.code);

                    match user_tunnel_ips.next_available_block(0, 2) {
                        Some(tunnel_net) => {
                            print!("tunnel_net: {} ", networkv4_to_string(&tunnel_net));

                            let mut tunnel_id: u16 = 0;
                            let mut dz_ip: IpV4 = [0, 0, 0, 0];

                            match user.user_type {
                                UserType::IBRLWithAllocatedIP | UserType::EdgeFiltering => {
                                    match device_state.get_next() {
                                        Some((xtunnel_id, xdz_ip)) => {
                                            tunnel_id = xtunnel_id;
                                            dz_ip = xdz_ip;
                                        }
                                        None => {}
                                    }
                                }
                                UserType::IBRL => {
                                    tunnel_id = device_state.get_next_tunnel_id().unwrap();
                                    dz_ip = user.client_ip;
                                }
                                UserType::Multicast => {}
                            }

                            if tunnel_id == 0 {
                                eprintln!("{}", "Error: No available tunnel block");

                                let res = RejectUserCommand {
                                    index: user.index,
                                    reason: "Error: No available tunnel block".to_string(),
                                }
                                .execute(client);

                                match res {
                                    Ok(signature) => println!("Rejected {}", signature.to_string()),
                                    Err(e) => println!("Error: {}", e.to_string()),
                                }
                                return;
                            }

                            print!(
                                "tunnel_id: {} dz_ip: {} ",
                                tunnel_id.to_string(),
                                ipv4_to_string(&dz_ip)
                            );

                            let res = ActivateUserCommand {
                                index: user.index,
                                tunnel_id: tunnel_id,
                                tunnel_net: tunnel_net,
                                dz_ip: dz_ip,
                            }
                            .execute(client);

                            match res {
                                Ok(signature) => println!("Activated   {}", signature.to_string()),
                                Err(e) => println!("Error: {}", e.to_string()),
                            }
                        }
                        None => {
                            println!("{}", "Error: No available user block");

                            let res = RejectUserCommand {
                                index: user.index,
                                reason: "Error: No available user block".to_string(),
                            }
                            .execute(client);

                            match res {
                                Ok(signature) => println!("Rejected {}", signature.to_string()),
                                Err(e) => println!("Error: {}", e.to_string()),
                            }
                        }
                    }
                }
                None => {
                    eprintln!("Error: Device not found {}", user.device_pk.to_string());

                    let res = RejectUserCommand {
                        index: user.index,
                        reason: "Error: Device not found".to_string(),
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => println!("Rejected {}", signature.to_string()),
                        Err(e) => println!("Error: {}", e.to_string()),
                    }
                }
            }
        }
        // Delete User
        UserStatus::Deleting | UserStatus::PendingBan => {
            print!("Deactivating User {} ", ipv4_to_string(&user.client_ip));

            if let Some(device_state) = devices.get_mut(&user.device_pk) {
                print!("for {} ", device_state.device.code);

                print!(
                    "tunnel_net: {} tunnel_id: {} dz_ip: {} ",
                    networkv4_to_string(&user.tunnel_net),
                    user.tunnel_id.to_string(),
                    ipv4_to_string(&user.dz_ip)
                );

                if user.tunnel_id != 0 {
                    tunnel_tunnel_ids.unassign(user.tunnel_id);
                }
                if user.tunnel_net != ([0, 0, 0, 0], 0) {
                    user_tunnel_ips.unassign_block(user.tunnel_net);
                }
                if user.dz_ip != [0, 0, 0, 0] {
                    device_state.release(user.dz_ip, user.tunnel_id);
                }

                if user.status == UserStatus::Deleting {
                    let res = DeactivateUserCommand {
                        index: user.index,
                        owner: user.owner,
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => println!("Deactivated {}", signature.to_string()),
                        Err(e) => println!("Error: {}", e.to_string()),
                    }
                } else if user.status == UserStatus::PendingBan {
                    let res = BanUserCommand { index: user.index }.execute(client);

                    match res {
                        Ok(signature) => println!("Banned {}", signature.to_string()),
                        Err(e) => println!("Error: {}", e.to_string()),
                    }
                }
            }
        }
        _ => {}
    }
}

#[cfg(test)]
mod tests {
    use doublezero_sdk::{
        AccountType, DeviceType, MockDoubleZeroClient, TunnelTunnelType, UserCYOA,
    };
    use doublezero_sla_program::{
        instructions::DoubleZeroInstruction,
        pda::get_globalstate_pda,
        processors::{
            device::{activate::DeviceActivateArgs, deactivate::DeviceDeactivateArgs},
            tunnel::reject::TunnelRejectArgs,
            user::{activate::UserActivateArgs, ban::UserBanArgs, reject::UserRejectArgs},
        },
        state::globalstate::GlobalState,
    };
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    use super::*;

    fn create_test_client() -> MockDoubleZeroClient {
        let mut client = MockDoubleZeroClient::new();

        // Program ID
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        // Global State
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed: 0,
            account_index: 0,
            foundation_allowlist: vec![],
            device_allowlist: vec![],
            user_allowlist: vec![],
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));
        client
            .expect_execute_transaction()
            .returning(|_, _| Ok(Signature::new_unique()));
        client
    }

    #[test]
    fn test_process_device_event_pending_to_deleted() {
        let mut devices = HashMap::new();

        let device_pubkey = Pubkey::new_unique();
        let mut device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 1],
            status: DeviceStatus::Pending,
            code: "TestDevice".to_string(),
            dz_prefixes: vec![([10, 0, 0, 1], 24), ([10, 0, 1, 1], 24)],
        };

        let mut client = create_test_client();
        client
            .expect_get_program_id()
            .returning(|| Pubkey::new_unique());

        let device2 = device.clone();
        client
            .expect_get()
            .with(predicate::eq(device_pubkey.clone()))
            .returning(move |_| Ok(AccountData::Device(device2.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs {
                    index: device.index,
                    bump_seed: device.bump_seed,
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        process_device_event(&client, &device_pubkey, &mut devices, &device);

        assert!(devices.contains_key(&device_pubkey));
        assert_eq!(devices.get(&device_pubkey).unwrap().device, device);

        device.status = DeviceStatus::Deleting;

        let mut client = create_test_client();
        client
            .expect_get_program_id()
            .returning(|| Pubkey::new_unique());
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeactivateDevice(
                    DeviceDeactivateArgs {
                        index: device.index,
                        bump_seed: device.bump_seed,
                    },
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        process_device_event(&client, &device_pubkey, &mut devices, &device);
        assert!(!devices.contains_key(&device_pubkey));
    }

    #[test]
    fn test_process_device_event_activated() {
        let mut devices = HashMap::new();
        let mut client = create_test_client();
        let pubkey = Pubkey::new_unique();

        let mut device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 1],
            status: DeviceStatus::Activated,
            code: "TestDevice".to_string(),
            dz_prefixes: vec![([10, 0, 0, 1], 24)],
        };

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs {
                    index: device.index,
                    bump_seed: device.bump_seed,
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        process_device_event(&client, &pubkey, &mut devices, &device);

        assert!(devices.contains_key(&pubkey));
        assert_eq!(devices.get(&pubkey).unwrap().device, device);

        device.dz_prefixes.push(([10, 0, 1, 1], 24));
        process_device_event(&client, &pubkey, &mut devices, &device);

        assert!(devices.contains_key(&pubkey));
        assert_eq!(devices.get(&pubkey).unwrap().device, device);
    }

    #[test]
    fn test_process_tunnel_event_pending_to_deleted() {
        let mut tunnel_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 16));
        let mut tunnel_tunnel_ids = IDAllocator::new(500, vec![500, 501, 503]);
        let mut client = create_test_client();

        let tunnel = Tunnel {
            account_type: AccountType::Tunnel,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            tunnel_type: TunnelTunnelType::MPLSoGRE,
            bandwidth: 10_000_000_000,
            mtu: 1500,
            delay_ns: 100,
            jitter_ns: 100,
            tunnel_id: 1,
            tunnel_net: ([0, 0, 0, 0], 0),
            status: TunnelStatus::Pending,
            code: "TestTunnel".to_string(),
        };

        client
            .expect_execute_transaction()
            .returning(|_, _| Ok(Signature::new_unique()));

        process_tunnel_event(
            &client,
            &mut tunnel_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &tunnel,
        );

        assert!(tunnel_tunnel_ids.assigned.contains(&502_u16));
        assert!(tunnel_tunnel_ips.contains([10, 0, 0, 42]));

        let mut tunnel = tunnel.clone();
        tunnel.status = TunnelStatus::Deleting;
        tunnel.tunnel_id = 502;
        tunnel.tunnel_net = ([10, 0, 0, 0], 31);

        client
            .expect_execute_transaction()
            .returning(|_, _| Ok(Signature::new_unique()));

        let assigned_ips = tunnel_tunnel_ips.assigned_ips.clone();

        process_tunnel_event(
            &client,
            &mut tunnel_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &tunnel,
        );

        assert!(!tunnel_tunnel_ids.assigned.contains(&502_u16));
        assert_ne!(tunnel_tunnel_ips.assigned_ips, assigned_ips);
    }

    #[test]
    fn test_process_tunnel_event_rejected() {
        let mut tunnel_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 32));
        let mut tunnel_tunnel_ids = IDAllocator::new(500, vec![500, 501, 503]);
        let mut client = create_test_client();

        let tunnel = Tunnel {
            account_type: AccountType::Tunnel,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            tunnel_type: TunnelTunnelType::MPLSoGRE,
            bandwidth: 10_000_000_000,
            mtu: 1500,
            delay_ns: 100,
            jitter_ns: 100,
            tunnel_id: 1,
            tunnel_net: ([0, 0, 0, 0], 0),
            status: TunnelStatus::Pending,
            code: "TestTunnel".to_string(),
        };

        let _ = tunnel_tunnel_ips.next_available_block(0, 2);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::RejectTunnel(TunnelRejectArgs {
                    index: tunnel.index,
                    bump_seed: tunnel.bump_seed,
                    reason: "Error: No available tunnel block".to_string(),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        process_tunnel_event(
            &client,
            &mut tunnel_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &tunnel,
        );
    }

    fn do_test_process_user_event_pending_to_activated(
        user_type: UserType,
        expected_dz_ip: Option<IpV4>,
    ) {
        let mut user_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 16));
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2],
            status: DeviceStatus::Activated,
            code: "TestDevice".to_string(),
            dz_prefixes: vec![([10, 0, 0, 1], 24)],
        };

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            user_type: user_type,
            tenant_pk: Pubkey::new_unique(),
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1],
            dz_ip: [0, 0, 0, 0],
            tunnel_id: 0,
            tunnel_net: ([0, 0, 0, 0], 0),
            status: UserStatus::Pending,
        };

        let device2 = device.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey.clone()))
            .returning(move |_| Ok(AccountData::Device(device2.clone())));

        let device2 = device.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Device))
            .returning(move |_| {
                let mut devices = HashMap::new();
                devices.insert(device_pubkey, AccountData::Device(device2.clone()));
                Ok(devices)
            });

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                    index: user.index,
                    bump_seed: user.bump_seed,
                    tunnel_id: 100,
                    tunnel_net: ([10, 0, 0, 0], 31),
                    dz_ip: expected_dz_ip.unwrap_or([0, 0, 0, 0]),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut devices = HashMap::new();
        devices.insert(device_pubkey, DeviceState::new(&device));

        process_user_event(
            &client,
            &user_pubkey,
            &mut devices,
            &mut user_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &user,
        );

        assert!(user_tunnel_ips.assigned_ips.len() > 0);
        assert!(tunnel_tunnel_ids.assigned.len() > 0);
    }

    #[test]
    fn test_process_user_event_pending_to_activated_ibrl() {
        do_test_process_user_event_pending_to_activated(UserType::IBRL, Some([192, 168, 1, 1]));
    }

    #[test]
    fn test_process_user_event_pending_to_activated_ibrl_with_allocated_ip() {
        do_test_process_user_event_pending_to_activated(UserType::IBRLWithAllocatedIP, None);
    }

    #[test]
    fn test_process_user_event_pending_to_activated_edge_filtering() {
        do_test_process_user_event_pending_to_activated(UserType::EdgeFiltering, None);
    }

    #[test]
    fn test_process_user_event_pending_to_rejected_by_get_device() {
        let mut user_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 32));
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2],
            status: DeviceStatus::Activated,
            code: "TestDevice".to_string(),
            dz_prefixes: vec![([10, 0, 0, 1], 24)],
        };

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            user_type: UserType::IBRLWithAllocatedIP,
            tenant_pk: Pubkey::new_unique(),
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1],
            dz_ip: [0, 0, 0, 0],
            tunnel_id: 0,
            tunnel_net: ([0, 0, 0, 0], 0),
            status: UserStatus::Pending,
        };

        client
            .expect_get()
            .with(predicate::eq(user_pubkey.clone()))
            .returning(move |_| Err(eyre::Report::msg("Error: Device not found")));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                    index: user.index,
                    bump_seed: user.bump_seed,
                    reason: "Error: Device not found".to_string(),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut devices = HashMap::new();
        devices.insert(device_pubkey, DeviceState::new(&device));

        process_user_event(
            &client,
            &user_pubkey,
            &mut devices,
            &mut user_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &user,
        );
    }

    #[test]
    fn test_process_user_event_pending_to_rejected_by_no_tunnel_block() {
        let mut user_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 32));
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2],
            status: DeviceStatus::Activated,
            code: "TestDevice".to_string(),
            dz_prefixes: vec![([10, 0, 0, 1], 24)],
        };

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            user_type: UserType::IBRLWithAllocatedIP,
            tenant_pk: Pubkey::new_unique(),
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1],
            dz_ip: [0, 0, 0, 0],
            tunnel_id: 0,
            tunnel_net: ([0, 0, 0, 0], 0),
            status: UserStatus::Pending,
        };

        let device2 = device.clone();
        client
            .expect_get()
            .with(predicate::eq(device_pubkey.clone()))
            .returning(move |_| Ok(AccountData::Device(device2.clone())));

        let device2 = device.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Device))
            .returning(move |_| {
                let mut devices = HashMap::new();
                devices.insert(device_pubkey, AccountData::Device(device2.clone()));
                Ok(devices)
            });

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                    index: user.index,
                    bump_seed: user.bump_seed,
                    reason: "Error: No available tunnel block".to_string(),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut devices = HashMap::new();
        let device2 = device.clone();
        devices.insert(device_pubkey, DeviceState::new(&device2));

        process_user_event(
            &client,
            &user_pubkey,
            &mut devices,
            &mut user_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &user,
        );
    }

    #[test]
    fn test_process_user_event_pending_to_rejected_by_no_user_block() {
        let mut user_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 32));
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        // eat a blocok
        let _ = user_tunnel_ips.next_available_block(0, 2);

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2],
            status: DeviceStatus::Activated,
            code: "TestDevice".to_string(),
            dz_prefixes: vec![([10, 0, 0, 1], 24)],
        };

        let pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            user_type: UserType::IBRLWithAllocatedIP,
            tenant_pk: Pubkey::new_unique(),
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1],
            dz_ip: [0, 0, 0, 0],
            tunnel_id: 0,
            tunnel_net: ([0, 0, 0, 0], 0),
            status: UserStatus::Pending,
        };

        let device2 = device.clone();
        client
            .expect_get()
            .with(predicate::eq(pubkey.clone()))
            .returning(move |_| Ok(AccountData::Device(device2.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                    index: user.index,
                    bump_seed: user.bump_seed,
                    reason: "Error: No available user block".to_string(),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut devices = HashMap::new();
        let device2 = device.clone();
        devices.insert(device_pubkey, DeviceState::new(&device2));

        process_user_event(
            &client,
            &pubkey,
            &mut devices,
            &mut user_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &user,
        );
    }

    fn do_test_process_user_event_deleting_or_pending_ban<F>(user_status: UserStatus, func: F)
    where
        F: Fn(&mut MockDoubleZeroClient, &User) -> (),
    {
        assert!(user_status == UserStatus::Deleting || user_status == UserStatus::PendingBan);

        let mut devices = HashMap::new();
        let mut user_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 16));
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            user_type: UserType::IBRLWithAllocatedIP,
            tenant_pk: Pubkey::new_unique(),
            device_pk: pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1],
            dz_ip: [0, 0, 0, 0],
            tunnel_id: 102,
            tunnel_net: ([10, 0, 0, 0], 31),
            status: user_status,
        };

        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2],
            status: DeviceStatus::Activated,
            code: "TestDevice".to_string(),
            dz_prefixes: vec![([11, 0, 0, 0], 16)],
        };

        devices.insert(pubkey, DeviceState::new(&device));

        func(&mut client, &user);

        assert!(tunnel_tunnel_ids.assigned.contains(&102));

        process_user_event(
            &client,
            &pubkey,
            &mut devices,
            &mut user_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &user,
        );

        assert!(!tunnel_tunnel_ids.assigned.contains(&102));
    }

    #[test]
    fn test_process_user_event_deleting() {
        do_test_process_user_event_deleting_or_pending_ban(
            UserStatus::Deleting,
            |user_service, user| {
                user_service
                    .expect_execute_transaction()
                    .with(
                        predicate::eq(DoubleZeroInstruction::DeactivateDevice(
                            DeviceDeactivateArgs {
                                index: user.index,
                                bump_seed: user.bump_seed,
                            },
                        )),
                        predicate::always(),
                    )
                    .returning(|_, _| Ok(Signature::new_unique()));
            },
        );
    }

    #[test]
    fn test_process_user_event_pending_ban() {
        do_test_process_user_event_deleting_or_pending_ban(
            UserStatus::PendingBan,
            |user_service, user| {
                user_service
                    .expect_execute_transaction()
                    .with(
                        predicate::eq(DoubleZeroInstruction::BanUser(UserBanArgs {
                            index: user.index,
                            bump_seed: user.bump_seed,
                        })),
                        predicate::always(),
                    )
                    .returning(|_, _| Ok(Signature::new_unique()));
            },
        );
    }
}
