use std::collections::HashMap;
use colored::Colorize;

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
        print!("Connected to url: {} ws: {} program_id: {} ", self.client.get_rpc().green(), self.client.get_ws().green(), self.client.get_program_id().to_string().green());

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

                self.user_tunnel_ips.assign_block(user.tunnel_net);
            });

        println!("devices: {} tunnels: {} users: {}", devices.len().to_string().green(), tunnels.len().to_string().green(), users.len().to_string().green());

        Ok(())
    }

    fn add_device(&mut self, pubkey: &Pubkey, device: &Device) {
        if !self.devices.contains_key(pubkey) {
            self.devices.insert(*pubkey, DeviceState::new(device));
        }
    }

    pub fn run(&mut self) -> eyre::Result<()> {
        self.devices.iter().for_each(|(_pubkey,device)| {

            print!("Device code: {} public_ip: {} dz_prefix: {} tunnels: ", device.device.code.green(), ipv4_to_string(&device.device.public_ip).green(), networkv4_to_string(&device.device.dz_prefix).green());            
            
            if device.tunnel_ids.assigned.len() == 0 {
                print!("{},", "-".green());
            }            
            device.tunnel_ids.assigned.iter().for_each(|tunnel_id| {
                print!("{},", tunnel_id.to_string().green());
            });
            println!("\x08 ");
        });

        print!("tunnel_net: {} assigned: ", self.user_tunnel_ips.base_block.to_string().green());
        if self.user_tunnel_ips.assigned_ips.len() == 0 {
            print!("{},", "-".green());
        }
        self.user_tunnel_ips.print_assigned_ips();
        println!("\x08 ");

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
                                        println!("Activated {}", signature.to_string().green());

                                        println!("Add Device: {} public_ip: {} dz_prefix: {} ", device.code.green(), ipv4_to_string(&device.public_ip).green(), networkv4_to_string(&device.dz_prefix).green());
                                        self.devices.insert(*pubkey, DeviceState::new(device));
                            
                                    },
                                    Err(e) => println!("Error: {}", e.to_string().red()),
                                }
                            }
                            DeviceStatus::Activated => {
                                if !self.devices.contains_key(pubkey) {
                                    println!("Add Device: {} public_ip: {} dz_prefix: {} ", device.code.green(), ipv4_to_string(&device.public_ip).green(), networkv4_to_string(&device.dz_prefix).green());

                                    self.devices
                                        .insert(*pubkey, DeviceState::new(device));
                                }
                            }
                            DeviceStatus::Deleting => {
                                print!("Deleting Device {} ", device.code.green());
                                match client.deactivate_device(device.index, device.owner) {
                                    Ok(signature) => {
                                        println!("Deactivated {}", signature.to_string().green());
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
                                print!("New Tunnel {} ", tunnel.code.green());
    
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
                                            Ok(signature) => println!("Activated {}", signature.to_string().green()),
                                            Err(e) => println!("Error: activate_tunnel: {}", e.to_string().red()),
                                        }
            
                                    },
                                    None => { 
                                        println!("{}", "Error: No available tunnel block".red());

                                        match self.client.reject_tunnel(
                                            tunnel.index, "Error: No available tunnel block".to_string()
                                        ) {
                                            Ok(signature) => println!("Rejected {}", signature.to_string().red()),
                                            Err(e) => println!("Error: reject_tunnel: {}", e.to_string().red()),
                                        }
                                },
                                }                            
                            },
                            TunnelStatus::Deleting => {
                                print!("Deleting Tunnel {} ", tunnel.code.green());

                                self.tunnel_tunnel_ids.unassign(tunnel.tunnel_id);
                                self.tunnel_tunnel_ips.unassign_block(tunnel.tunnel_net);
    
                                match client.deactivate_tunnel(tunnel.index, tunnel.owner) {
                                    Ok(signature) => println!("Deactivated {}", signature.to_string().green()),
                                    Err(e) => println!("{}: {}", "Error: deactivate_tunnel:".red(), e.to_string().red()),
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
                                print!("Activating User   {} ", ipv4_to_string(&user.client_ip).green());
                                // Load Device if not exists
                                if !self.devices.contains_key(&user.device_pk) {
                                    match client.get_device(&user.device_pk) {
                                        Ok(device) => {
                                            println!("Add Device: {} public_ip: {} dz_prefix: {} ", device.code.green(), ipv4_to_string(&device.public_ip).green(), networkv4_to_string(&device.dz_prefix).green());

                                            self.devices
                                                .insert(*pubkey, DeviceState::new(&device));
                                            }
                                        Err(e) => {
                                            println!("Error: {}", e.to_string().red());
                                        }
                                    }
                                }

                                match self.devices.get_mut(&user.device_pk) {
                                    Some(device_state) => {
                                        print!("for {} ", device_state.device.code.green());

                                        match self
                                            .user_tunnel_ips
                                            .next_available_block(0, 2) {
                                                Some(tunnel_net) => {
        
                                                    print!("tunnel_net: {} ", networkv4_to_string(&tunnel_net).green());
                                                    match device_state.get_next() {
                                                        Some((tunnel_id, dz_ip)) => {
        
                                                            print!("tunnel_id: {} tunnel_id: {} ", tunnel_id.to_string().green(), ipv4_to_string(&dz_ip).green());
        
                                                            match client.activate_user(
                                                                user.index,
                                                                tunnel_id,
                                                                tunnel_net,
                                                                dz_ip,
                                                            ) {
                                                                Ok(signature) => println!("Activated   {}", signature.to_string().green()),
                                                                Err(e) => println!("Error: {}", e.to_string().red()),
                                                            }        
                                                        },
                                                        None => {
                                                            eprintln!("{}", "Error: No available tunnel block".red());
        
                                                            match client.reject_user(
                                                                user.index,
                                                                "Error: No available tunnel block".to_string()) {
                                                                Ok(signature) => println!("Rejected {}", signature.to_string().green()),
                                                                Err(e) => println!("Error: {}", e.to_string().red()),
                                                            }        
                                                        }
                                                    }                                     
                        
                                                }, 
                                                None => { 
                                                    println!("{}", "Error: No available user block".red());
        
                                                match client.reject_user(
                                                    user.index,
                                                    "Error: No available user block".to_string()) {
                                                    Ok(signature) => println!("Rejected {}", signature.to_string().green()),
                                                    Err(e) => println!("Error: {}", e.to_string().red()),
                                                }                                            
                                            }
                                        }                                                
                                    },
                                    None => {
                                        eprintln!("Error: Device not found {}", user.device_pk.to_string().red());
                                        match client.reject_user(
                                            user.index,
                                            "Error: Device not found".to_string()) {
                                            Ok(signature) => println!("Rejected {}", signature.to_string().green()),
                                            Err(e) => println!("Error: {}", e.to_string().red()),
                                        }                                        
                                    }
                                }                                                            
                            },
                            // Delete User
                            UserStatus::Deleting | UserStatus::PendingBan => {
                                print!("Deactivating User {} ", ipv4_to_string(&user.client_ip).green());

                                if let Some(device_state) = self.devices.get_mut(&user.device_pk) {
                                    print!("for {} ", device_state.device.code.green());

                                    print!("tunnel_net: {} tunnel_id: {} tunnel_id: {} ", networkv4_to_string(&user.tunnel_net).green(), user.tunnel_id.to_string().green(), ipv4_to_string(&user.dz_ip).green());

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
                                            Ok(signature) => println!("Deactivated {}", signature.to_string().green()),
                                            Err(e) => println!("Error: {}", e.to_string().red()),
                                        }
                                    } else if user.status == UserStatus::PendingBan {
                                        match client.ban_user(user.index) {
                                            Ok(signature) => println!("Banned {}", signature.to_string().green()),
                                            Err(e) => println!("Error: {}", e.to_string().red()),
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
