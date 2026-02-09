use crate::{idallocator::IDAllocator, ipblockallocator::IPBlockAllocator};
use doublezero_sdk::Device;
use ipnetwork::Ipv4Network;
use log::info;
use std::net::Ipv4Addr;

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
            dz_ips: device
                .dz_prefixes
                .iter()
                .map(|b| IPBlockAllocator::new((*b).into()))
                .collect(),
            tunnel_ids: IDAllocator::new(500, vec![]),
        }
    }

    pub fn update(&mut self, device: &Device) {
        if self.device.dz_prefixes != device.dz_prefixes {
            self.device = device.clone();
            self.dz_ips = device
                .dz_prefixes
                .iter()
                .map(|b| IPBlockAllocator::new((*b).into()))
                .collect();

            info!(
                "Update Device: {} public_ip: {} dz_prefixes: {} ",
                self.device.code, &self.device.public_ip, &self.device.dz_prefixes,
            );
        }
    }

    pub fn get_next_dz_ip(&mut self) -> Option<Ipv4Addr> {
        for allocator in self.dz_ips.iter_mut() {
            match allocator.next_available_block(2, 1) {
                Some(dz_ip) => {
                    return Some(dz_ip.ip());
                }
                None => continue,
            }
        }

        None
    }

    pub fn get_next_tunnel_id(&mut self) -> u16 {
        self.tunnel_ids.next_available()
    }

    pub fn register(&mut self, dz_ip: Ipv4Addr, tunnel_id: u16) -> eyre::Result<()> {
        for allocator in self.dz_ips.iter_mut() {
            if allocator.contains(dz_ip) {
                allocator.assign_block(Ipv4Network::new(dz_ip, 32)?);
            }
        }
        self.tunnel_ids.assign(tunnel_id);

        Ok(())
    }

    pub fn release(&mut self, dz_ip: Ipv4Addr, tunnel_id: u16) -> eyre::Result<()> {
        for allocator in self.dz_ips.iter_mut() {
            if allocator.contains(dz_ip) {
                allocator.unassign_block(Ipv4Network::new(dz_ip, 32)?);
            }
        }
        self.tunnel_ids.unassign(tunnel_id);

        Ok(())
    }
}
