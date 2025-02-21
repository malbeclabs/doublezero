use double_zero_sdk::{Device, IpV4};
use crate::{idallocator::IDAllocator, ipblockallocator::IPBlockAllocator};

#[derive(Debug)]
pub struct DeviceState {
    pub device: Device,

    pub tunnel_ids: IDAllocator,
    pub dz_ips: IPBlockAllocator,
}

impl DeviceState {
    pub fn new(device: &Device) -> DeviceState {
        DeviceState {
            device: device.clone(),
            dz_ips: IPBlockAllocator::new(device.dz_prefix),
            tunnel_ids: IDAllocator::new(500, vec![]),
        }
    }

    pub fn get_next(&mut self) -> Option<(u16, IpV4)> {
        match self
            .dz_ips
            .next_available_block(1, 1)
        {
            Some(dz_ip) => {
                let tunnel_id = self.tunnel_ids.next_available();
                Some((tunnel_id, dz_ip.0))
            }
            None => None,
        }
    }

    pub fn register(&mut self, dz_ip: IpV4, tunnel_id: u16) {
        self.dz_ips.assign_block((dz_ip, 32));
        self.tunnel_ids.assign(tunnel_id);
    }

    pub fn release(&mut self, dz_ip: IpV4, tunnel_id: u16) {
        self.dz_ips.unassign_block((dz_ip, 32));
        self.tunnel_ids.unassign(tunnel_id);
    }
}
