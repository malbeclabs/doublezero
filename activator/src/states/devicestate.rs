use double_zero_sdk::{ipv4_to_string, networkv4_list_to_string, Device, IpV4};
use crate::{idallocator::IDAllocator, ipblockallocator::IPBlockAllocator};

#[derive(Debug)]
pub struct DeviceState {
    pub device: Device,

    pub tunnel_ids: IDAllocator,
    pub dz_ips: Vec<IPBlockAllocator>,
}

impl DeviceState {
    pub fn new(device: &Device) -> DeviceState {
        DeviceState {
            device: device.clone(),
            dz_ips: device.dz_prefixes.iter().map(|b| IPBlockAllocator::new(*b)).collect(),
            tunnel_ids: IDAllocator::new(500, vec![]),
        }
    }

    pub fn update(&mut self, device: &Device) {

        if self.device.dz_prefixes != device.dz_prefixes {
            self.device = device.clone();
            self.dz_ips = device.dz_prefixes.iter().map(|b| IPBlockAllocator::new(*b)).collect();

            println!("Update Device: {} public_ip: {} dz_prefixes: {} ", self.device.code, ipv4_to_string(&self.device.public_ip), networkv4_list_to_string(&self.device.dz_prefixes));
        }
    }

    pub fn get_next(&mut self) -> Option<(u16, IpV4)> {

        for allocator in self.dz_ips.iter_mut() {
            match allocator.next_available_block(1, 1) {
                Some(dz_ip) => {
                    let tunnel_id = self.tunnel_ids.next_available();
                    return Some((tunnel_id, dz_ip.0));
                }
                None => continue,
            }
        }  

        None    
    }

    pub fn register(&mut self, dz_ip: IpV4, tunnel_id: u16) {
        for allocator in self.dz_ips.iter_mut() {
            if allocator.contains(dz_ip) {
                allocator.assign_block((dz_ip, 32));
            }
        }
        self.tunnel_ids.assign(tunnel_id);
    }

    pub fn release(&mut self, dz_ip: IpV4, tunnel_id: u16) {
        for allocator in self.dz_ips.iter_mut() {
            if allocator.contains(dz_ip) {
                allocator.unassign_block((dz_ip, 32));
            }
        }
        self.tunnel_ids.unassign(tunnel_id);
    }
}
