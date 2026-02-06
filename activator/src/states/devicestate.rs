use crate::{idallocator::IDAllocator, ipblockallocator::IPBlockAllocator};
use doublezero_sdk::Device;
use ipnetwork::Ipv4Network;
use log::info;
use std::{collections::HashMap, net::Ipv4Addr};

#[derive(Debug)]
pub struct DeviceState {
    pub device: Device,

    pub tunnel_ids: IDAllocator,
    pub dz_ips: Vec<IPBlockAllocator>,
    /// Tracks which tunnel endpoints are in use per client_ip.
    /// Key: client_ip, Value: tunnel_endpoint IP used by that client.
    /// This allows multiple tunnels from the same client to use different endpoints.
    tunnel_endpoints_in_use: HashMap<Ipv4Addr, Vec<Ipv4Addr>>,
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
            tunnel_endpoints_in_use: HashMap::new(),
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
            match allocator.next_available_block(1, 1) {
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

    /// Get an available tunnel endpoint for the given client_ip.
    /// Returns a UserTunnelEndpoint interface IP that is not already in use by this client,
    /// or falls back to the device's public_ip if no dedicated endpoints are available.
    pub fn get_available_tunnel_endpoint(&self, client_ip: Ipv4Addr) -> Ipv4Addr {
        // Get all UserTunnelEndpoint interfaces from the device
        let tunnel_endpoints: Vec<Ipv4Addr> = self
            .device
            .interfaces
            .iter()
            .filter_map(|iface| {
                let iface = iface.into_current_version();
                if iface.user_tunnel_endpoint && iface.ip_net != Default::default() {
                    Some(iface.ip_net.ip())
                } else {
                    None
                }
            })
            .collect();

        // If no UserTunnelEndpoint interfaces configured, use device public_ip
        if tunnel_endpoints.is_empty() {
            return self.device.public_ip;
        }

        // Get endpoints already in use by this client_ip
        let in_use = self
            .tunnel_endpoints_in_use
            .get(&client_ip)
            .cloned()
            .unwrap_or_default();

        // Find an endpoint not in use by this client
        for endpoint in &tunnel_endpoints {
            if !in_use.contains(endpoint) {
                return *endpoint;
            }
        }

        // All endpoints in use, fall back to device public_ip
        // (This shouldn't happen if device has enough endpoints configured)
        self.device.public_ip
    }

    /// Register a tunnel endpoint as in use for a client_ip.
    pub fn register_tunnel_endpoint(&mut self, client_ip: Ipv4Addr, tunnel_endpoint: Ipv4Addr) {
        self.tunnel_endpoints_in_use
            .entry(client_ip)
            .or_default()
            .push(tunnel_endpoint);
    }

    /// Release a tunnel endpoint for a client_ip.
    pub fn release_tunnel_endpoint(&mut self, client_ip: Ipv4Addr, tunnel_endpoint: Ipv4Addr) {
        if let Some(endpoints) = self.tunnel_endpoints_in_use.get_mut(&client_ip) {
            endpoints.retain(|ep| *ep != tunnel_endpoint);
            if endpoints.is_empty() {
                self.tunnel_endpoints_in_use.remove(&client_ip);
            }
        }
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

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_sdk::{
        CurrentInterfaceVersion, Device, InterfaceStatus, InterfaceType, LoopbackType,
    };
    use std::net::Ipv4Addr;

    fn create_test_device_with_tunnel_endpoints(endpoints: Vec<Ipv4Addr>) -> Device {
        let interfaces = endpoints
            .into_iter()
            .enumerate()
            .map(|(i, ip)| {
                CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: format!("Loopback{}", 100 + i),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::None,
                    vlan_id: 0,
                    ip_net: format!("{}/32", ip).parse().unwrap(),
                    node_segment_idx: 0,
                    user_tunnel_endpoint: true,
                    ..Default::default()
                }
                .to_interface()
            })
            .collect();

        Device {
            public_ip: Ipv4Addr::new(1, 1, 1, 1),
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            interfaces,
            ..Default::default()
        }
    }

    fn create_test_device_without_tunnel_endpoints() -> Device {
        Device {
            public_ip: Ipv4Addr::new(1, 1, 1, 1),
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            interfaces: vec![],
            ..Default::default()
        }
    }

    #[test]
    fn test_get_available_tunnel_endpoint_no_endpoints_configured() {
        let device = create_test_device_without_tunnel_endpoints();
        let state = DeviceState::new(&device);

        let client_ip = Ipv4Addr::new(2, 2, 2, 2);
        let endpoint = state.get_available_tunnel_endpoint(client_ip);

        // Should fall back to device public_ip
        assert_eq!(endpoint, Ipv4Addr::new(1, 1, 1, 1));
    }

    #[test]
    fn test_get_available_tunnel_endpoint_first_tunnel() {
        let device = create_test_device_with_tunnel_endpoints(vec![
            Ipv4Addr::new(5, 5, 5, 5),
            Ipv4Addr::new(6, 6, 6, 6),
        ]);
        let state = DeviceState::new(&device);

        let client_ip = Ipv4Addr::new(2, 2, 2, 2);
        let endpoint = state.get_available_tunnel_endpoint(client_ip);

        // Should return first available endpoint
        assert_eq!(endpoint, Ipv4Addr::new(5, 5, 5, 5));
    }

    #[test]
    fn test_get_available_tunnel_endpoint_second_tunnel_same_client() {
        let device = create_test_device_with_tunnel_endpoints(vec![
            Ipv4Addr::new(5, 5, 5, 5),
            Ipv4Addr::new(6, 6, 6, 6),
        ]);
        let mut state = DeviceState::new(&device);

        let client_ip = Ipv4Addr::new(2, 2, 2, 2);

        // First tunnel uses first endpoint
        let first_endpoint = state.get_available_tunnel_endpoint(client_ip);
        assert_eq!(first_endpoint, Ipv4Addr::new(5, 5, 5, 5));
        state.register_tunnel_endpoint(client_ip, first_endpoint);

        // Second tunnel from same client should use second endpoint
        let second_endpoint = state.get_available_tunnel_endpoint(client_ip);
        assert_eq!(second_endpoint, Ipv4Addr::new(6, 6, 6, 6));
    }

    #[test]
    fn test_get_available_tunnel_endpoint_different_clients() {
        let device = create_test_device_with_tunnel_endpoints(vec![
            Ipv4Addr::new(5, 5, 5, 5),
            Ipv4Addr::new(6, 6, 6, 6),
        ]);
        let mut state = DeviceState::new(&device);

        let client1 = Ipv4Addr::new(2, 2, 2, 2);
        let client2 = Ipv4Addr::new(3, 3, 3, 3);

        // First client uses first endpoint
        let endpoint1 = state.get_available_tunnel_endpoint(client1);
        assert_eq!(endpoint1, Ipv4Addr::new(5, 5, 5, 5));
        state.register_tunnel_endpoint(client1, endpoint1);

        // Different client can also use first endpoint (no conflict)
        let endpoint2 = state.get_available_tunnel_endpoint(client2);
        assert_eq!(endpoint2, Ipv4Addr::new(5, 5, 5, 5));
    }

    #[test]
    fn test_get_available_tunnel_endpoint_all_exhausted() {
        let device = create_test_device_with_tunnel_endpoints(vec![Ipv4Addr::new(5, 5, 5, 5)]);
        let mut state = DeviceState::new(&device);

        let client_ip = Ipv4Addr::new(2, 2, 2, 2);

        // Use the only endpoint
        let first_endpoint = state.get_available_tunnel_endpoint(client_ip);
        state.register_tunnel_endpoint(client_ip, first_endpoint);

        // Should fall back to device public_ip
        let second_endpoint = state.get_available_tunnel_endpoint(client_ip);
        assert_eq!(second_endpoint, Ipv4Addr::new(1, 1, 1, 1));
    }

    #[test]
    fn test_release_tunnel_endpoint() {
        let device = create_test_device_with_tunnel_endpoints(vec![Ipv4Addr::new(5, 5, 5, 5)]);
        let mut state = DeviceState::new(&device);

        let client_ip = Ipv4Addr::new(2, 2, 2, 2);
        let endpoint = Ipv4Addr::new(5, 5, 5, 5);

        // Register then release
        state.register_tunnel_endpoint(client_ip, endpoint);
        state.release_tunnel_endpoint(client_ip, endpoint);

        // Should be available again
        let available = state.get_available_tunnel_endpoint(client_ip);
        assert_eq!(available, endpoint);
    }

    #[test]
    fn test_release_tunnel_endpoint_multiple() {
        let device = create_test_device_with_tunnel_endpoints(vec![
            Ipv4Addr::new(5, 5, 5, 5),
            Ipv4Addr::new(6, 6, 6, 6),
        ]);
        let mut state = DeviceState::new(&device);

        let client_ip = Ipv4Addr::new(2, 2, 2, 2);
        let endpoint1 = Ipv4Addr::new(5, 5, 5, 5);
        let endpoint2 = Ipv4Addr::new(6, 6, 6, 6);

        // Register both
        state.register_tunnel_endpoint(client_ip, endpoint1);
        state.register_tunnel_endpoint(client_ip, endpoint2);

        // Release first one
        state.release_tunnel_endpoint(client_ip, endpoint1);

        // First endpoint should be available again
        let available = state.get_available_tunnel_endpoint(client_ip);
        assert_eq!(available, endpoint1);
    }
}
