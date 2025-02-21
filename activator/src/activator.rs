use std::collections::HashMap;
use colored::*;

use double_zero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use std::thread;
use std::time::Duration;

use crate::{
    idallocator::IDAllocator, ipblockallocator::IPBlockAllocator, states::devicestate::DeviceState,
};

pub struct Activator {
    pub client: DZClient,

    pub tunnel_tunnel_ids: IDAllocator,
    pub tunnel_tunnel_ips: IPBlockAllocator,

    pub user_tunnel_ips: IPBlockAllocator,
    pub devices: HashMap<Pubkey, DeviceState>,
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
        println!("Connected to\turl: {}\tws: {}\tprogram_id: {}", self.client.get_rpc().red(), self.client.get_ws().red(), self.client.get_program_id().to_string().red());

        // Fetch the list of tunnels, devices, and users from the client
        let devices = self.client.get_devices()?;
        let tunnels = self.client.get_tunnels()?;
        let users = self.client.get_users()?;

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
            });

        Ok(())
    }

    fn add_device(&mut self, pubkey: &Pubkey, device: &Device) {
        if !self.devices.contains_key(pubkey) {
            println!("Add Device: [{}] public_ip: [{}] dz_prefix: [{}] ", device.code, ipv4_to_string(&device.public_ip), networkv4_to_string(&device.dz_prefix));

            self.devices.insert(*pubkey, DeviceState::new(device));
        }
    }

    pub fn run(&mut self) -> eyre::Result<()> {
        self.client
            .gets_and_subscribe(|client, pubkey, data| {
                match data {
                    /*********************************************************************************************************************/
                    // DEVICE
                    /**********************************************************************************************************************/
                    AccountData::Device(device) => {
                        // Device Event
                        match device.status {
                            DeviceStatus::Pending => {
                                print!("New Device [{}] ", device.code);
                                match client.activate_device(device.index) {
                                    Ok(signature) => {
                                        println!("Activated {}", signature);

                                        println!("Add Device: [{}] public_ip: [{}] dz_prefix: [{}] ", device.code, ipv4_to_string(&device.public_ip), networkv4_to_string(&device.dz_prefix));
                                        self.devices.insert(*pubkey, DeviceState::new(device));
                            
                                    },
                                    Err(e) => println!("Error: {}", e),
                                }
                            }
                            DeviceStatus::Activated => {
                                if !self.devices.contains_key(pubkey) {
                                    println!("Add Device: [{}] public_ip: [{}] dz_prefix: [{}] ", device.code, ipv4_to_string(&device.public_ip), networkv4_to_string(&device.dz_prefix));

                                    self.devices
                                        .insert(*pubkey, DeviceState::new(device));
                                }
                            }
                            DeviceStatus::Deleting => {
                                print!("Deleting Device [{}] ", device.code);
                                match client.deactivate_device(device.index, device.owner) {
                                    Ok(signature) => {
                                        println!("Deactivated {}", signature);
                                        self.devices.remove(pubkey);
                                    },
                                    Err(e) => println!("Error: {}", e),
                                }
                            }
                            _ => {}
                        }
                    }
                    /**********************************************************************************************************************/
                    // TUNNEL
                    /**********************************************************************************************************************/
                    AccountData::Tunnel(tunnel) => {
                        match tunnel.status {
                            TunnelStatus::Pending => {
                                print!("New Tunnel [{}] ", tunnel.code);
    
                                match self
                                    .tunnel_tunnel_ips
                                    .next_available_block(0, 2) {
                                    Some(tunnel_net) => {
                                        let tunnel_id = self.tunnel_tunnel_ids.next_available();
    
                                        match self.client.activate_tunnel(
                                            tunnel.index,
                                            tunnel_id,
                                            tunnel_net,
                                        ) {
                                            Ok(signature) => println!("Activated {}", signature),
                                            Err(e) => println!("Error: {}", e),
                                        }
            
                                    },
                                    None => { 
                                        println!("Error: No available tunnel block");

                                        match self.client.reject_tunnel(
                                            tunnel.index, "Error: No available tunnel block".to_string()
                                        ) {
                                            Ok(signature) => println!("Rejected {}", signature),
                                            Err(e) => println!("Error: {}", e),
                                        }
                                },
                                }                            
                            },
                            TunnelStatus::Deleting => {
                                print!("Deleting Tunnel [{}] ", tunnel.code);
                                self.tunnel_tunnel_ids.unassign(tunnel.tunnel_id);
                                self.tunnel_tunnel_ips.unassign_block(tunnel.tunnel_net);
    
                                match client.deactivate_tunnel(tunnel.index, tunnel.owner) {
                                    Ok(signature) => println!("Deactivated {}", signature),
                                    Err(e) => println!("Error: {}", e),
                                }
                            },
                            _ => {}
                        }
                    }
                    /**********************************************************************************************************************/
                    // USER
                    /**********************************************************************************************************************/
                    AccountData::User(user) => {
                        // User Event
                        match user.status {
                            // Create User
                            UserStatus::Pending => {
                                print!("Activating User [{}] ", ipv4_to_string(&user.client_ip));
                                // Load Device if not exists
                                if !self.devices.contains_key(&user.device_pk) {
                                    match client.get_device(&user.device_pk) {
                                        Ok(device) => {
                                            println!("Add Device: [{}] public_ip: [{}] dz_prefix: [{}] ", device.code, ipv4_to_string(&device.public_ip), networkv4_to_string(&device.dz_prefix));

                                            self.devices
                                                .insert(*pubkey, DeviceState::new(&device));
                                            }
                                        Err(e) => {
                                            println!("Error: {}", e);
                                        }
                                    }
                                }

                                let device_state = self.devices.get_mut(&user.device_pk).unwrap();                            
                                print!("for [{}] ", device_state.device.code);

                                match self
                                    .user_tunnel_ips
                                    .next_available_block(0, 2) {
                                        Some(tunnel_net) => {

                                            print!("tunnel_net: [{}] ", networkv4_to_string(&tunnel_net));
                                            match device_state.get_next() {
                                                Some((tunnel_id, dz_ip)) => {

                                                    print!("tunnel_id: [{}] tunnel_id: [{}] ", tunnel_id, ipv4_to_string(&dz_ip));

                                                    match client.activate_user(
                                                        user.index,
                                                        tunnel_id,
                                                        tunnel_net,
                                                        dz_ip,
                                                    ) {
                                                        Ok(signature) => println!("Activated {}", signature),
                                                        Err(e) => println!("Error: {}", e),
                                                    }        
                                                },
                                                None => {
                                                    eprintln!("Error: No available tunnel block");   

                                                    match client.reject_user(
                                                        user.index,
                                                        "Error: No available tunnel block".to_string()) {
                                                        Ok(signature) => println!("Rejected {}", signature),
                                                        Err(e) => println!("Error: {}", e),
                                                    }        
                                                }
                                            }                                     
                
                                        }, 
                                        None => { println!("Error: No available user block");

                                        match client.reject_user(
                                            user.index,
                                            "Error: No available user block".to_string()) {
                                            Ok(signature) => println!("Rejected {}", signature),
                                            Err(e) => println!("Error: {}", e),
                                        }                                            
                                    }
                                }
                                
                            },
                            // Delete User
                            UserStatus::Deleting | UserStatus::PendingBan => {
                                print!("freeing up resources User [{}] ", ipv4_to_string(&user.client_ip));

                                if let Some(device_state) = self.devices.get_mut(&user.device_pk) {
                                    print!("for [{}] ", device_state.device.code);

                                    print!("tunnel_net: [{}] ", networkv4_to_string(&user.tunnel_net));
                                    print!("tunnel_id: [{}] tunnel_id: [{}] ", user.tunnel_id, ipv4_to_string(&user.dz_ip));

                                    if user.tunnel_id != 0 {
                                        self.tunnel_tunnel_ids.unassign(user.tunnel_id);
                                    }
                                    if user.tunnel_net != ([0, 0, 0, 0], 0) {
                                        self.user_tunnel_ips.unassign_block(user.tunnel_net);
                                    }
                                    if user.dz_ip != [0, 0, 0, 0] {
                                        device_state.release(user.dz_ip, user.tunnel_id);
                                    }

                                    if user.status == UserStatus::Deleting {
                                        match client.deactivate_user(user.index, user.owner) {
                                            Ok(signature) => println!("Deactivated {}", signature),
                                            Err(e) => println!("Error: {}", e),
                                        }
                                    } else if user.status == UserStatus::PendingBan {
                                        match client.ban_user(user.index) {
                                            Ok(signature) => println!("Banned {}", signature),
                                            Err(e) => println!("Error: {}", e),
                                        }
                                    }
                                }
                            }
                            _ => {}
                        }
                    }
                    _ => {}
                }
            })
            ?;
        Ok(())
    }
}
