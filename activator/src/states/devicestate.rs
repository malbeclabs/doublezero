use crate::{idallocator::IDAllocator, ipblockallocator::IPBlockAllocator};
use doublezero_sdk::Device;
use ipnetwork::Ipv4Network;
use log::info;
use std::{collections::HashMap, net::Ipv4Addr};

/// Stateless device state for onchain allocation mode.
/// Does not track tunnel IDs or DZ IPs locally - allocation is handled by the smart contract.
#[derive(Debug, Clone)]
pub struct DeviceStateStateless {
    pub device: Device,
}

impl DeviceStateStateless {
    pub fn new(device: &Device) -> Self {
        Self {
            device: device.clone(),
        }
    }

    pub fn update(&mut self, device: &Device) {
        if self.device.dz_prefixes != device.dz_prefixes {
            self.device = device.clone();
            info!(
                "Update Device: {} public_ip: {} dz_prefixes: {} ",
                self.device.code, &self.device.public_ip, &self.device.dz_prefixes,
            );
        }
    }
}

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
            self.dz_ips = device
                .dz_prefixes
                .iter()
                .map(|b| IPBlockAllocator::new((*b).into()))
                .collect();

            info!(
                "Update Device: {} public_ip: {} dz_prefixes: {} ",
                device.code, &device.public_ip, &device.dz_prefixes,
            );
        }
        // Always refresh the device data so interfaces (e.g. UTE loopbacks
        // added after initial load) are visible to get_available_tunnel_endpoint.
        self.device = device.clone();
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
    /// Returns `None` when all endpoints (including public_ip) are already in use by this client.
    pub fn get_available_tunnel_endpoint(&self, client_ip: Ipv4Addr) -> Option<Ipv4Addr> {
        // Get endpoints already in use by this client_ip
        let in_use = self
            .tunnel_endpoints_in_use
            .get(&client_ip)
            .cloned()
            .unwrap_or_default();

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

        // If no UserTunnelEndpoint interfaces configured, use device public_ip if available
        if tunnel_endpoints.is_empty() {
            if in_use.contains(&self.device.public_ip) {
                return None;
            }
            return Some(self.device.public_ip);
        }

        // Find an endpoint not in use by this client
        for endpoint in &tunnel_endpoints {
            if !in_use.contains(endpoint) {
                return Some(*endpoint);
            }
        }

        // All dedicated endpoints in use, fall back to device public_ip if available
        if in_use.contains(&self.device.public_ip) {
            return None;
        }
        Some(self.device.public_ip)
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

    /// Check whether the given IP is a valid tunnel endpoint for this device.
    /// Returns true if the IP matches the device's public_ip, or if it matches
    /// any interface that has `user_tunnel_endpoint == true` and a valid `ip_net`.
    pub fn is_valid_tunnel_endpoint(&self, ip: Ipv4Addr) -> bool {
        if ip == self.device.public_ip {
            return true;
        }

        self.device.interfaces.iter().any(|iface| {
            let iface = iface.into_current_version();
            iface.user_tunnel_endpoint
                && iface.ip_net != Default::default()
                && iface.ip_net.ip() == ip
        })
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

    fn create_test_device_with_non_ute_interface(ip: Ipv4Addr) -> Device {
        let interfaces = vec![CurrentInterfaceVersion {
            status: InterfaceStatus::Activated,
            name: "Loopback200".to_string(),
            interface_type: InterfaceType::Loopback,
            loopback_type: LoopbackType::None,
            vlan_id: 0,
            ip_net: format!("{}/32", ip).parse().unwrap(),
            node_segment_idx: 0,
            user_tunnel_endpoint: false,
            ..Default::default()
        }
        .to_interface()];

        Device {
            public_ip: Ipv4Addr::new(1, 1, 1, 1),
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            interfaces,
            ..Default::default()
        }
    }

    #[test]
    fn test_is_valid_tunnel_endpoint_matches_public_ip() {
        let device = create_test_device_without_tunnel_endpoints();
        let state = DeviceState::new(&device);

        assert!(state.is_valid_tunnel_endpoint(Ipv4Addr::new(1, 1, 1, 1)));
    }

    #[test]
    fn test_is_valid_tunnel_endpoint_matches_ute_interface() {
        let device = create_test_device_with_tunnel_endpoints(vec![
            Ipv4Addr::new(5, 5, 5, 5),
            Ipv4Addr::new(6, 6, 6, 6),
        ]);
        let state = DeviceState::new(&device);

        assert!(state.is_valid_tunnel_endpoint(Ipv4Addr::new(5, 5, 5, 5)));
        assert!(state.is_valid_tunnel_endpoint(Ipv4Addr::new(6, 6, 6, 6)));
    }

    #[test]
    fn test_is_valid_tunnel_endpoint_no_match() {
        let device = create_test_device_with_tunnel_endpoints(vec![Ipv4Addr::new(5, 5, 5, 5)]);
        let state = DeviceState::new(&device);

        assert!(!state.is_valid_tunnel_endpoint(Ipv4Addr::new(9, 9, 9, 9)));
    }

    #[test]
    fn test_is_valid_tunnel_endpoint_non_ute_interface() {
        let device = create_test_device_with_non_ute_interface(Ipv4Addr::new(7, 7, 7, 7));
        let state = DeviceState::new(&device);

        // The IP exists on an interface but user_tunnel_endpoint is false
        assert!(!state.is_valid_tunnel_endpoint(Ipv4Addr::new(7, 7, 7, 7)));
    }

    #[test]
    fn test_get_available_tunnel_endpoint_no_endpoints_configured() {
        let device = create_test_device_without_tunnel_endpoints();
        let state = DeviceState::new(&device);

        let client_ip = Ipv4Addr::new(2, 2, 2, 2);
        let endpoint = state.get_available_tunnel_endpoint(client_ip);

        // Should fall back to device public_ip
        assert_eq!(endpoint, Some(Ipv4Addr::new(1, 1, 1, 1)));
    }

    #[test]
    fn test_get_available_tunnel_endpoint_no_endpoints_configured_public_ip_in_use() {
        let device = create_test_device_without_tunnel_endpoints();
        let mut state = DeviceState::new(&device);

        let client_ip = Ipv4Addr::new(2, 2, 2, 2);
        // Register public_ip as in use
        state.register_tunnel_endpoint(client_ip, device.public_ip);

        let endpoint = state.get_available_tunnel_endpoint(client_ip);

        // public_ip already in use, should return None
        assert_eq!(endpoint, None);
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
        assert_eq!(endpoint, Some(Ipv4Addr::new(5, 5, 5, 5)));
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
        assert_eq!(first_endpoint, Some(Ipv4Addr::new(5, 5, 5, 5)));
        state.register_tunnel_endpoint(client_ip, first_endpoint.unwrap());

        // Second tunnel from same client should use second endpoint
        let second_endpoint = state.get_available_tunnel_endpoint(client_ip);
        assert_eq!(second_endpoint, Some(Ipv4Addr::new(6, 6, 6, 6)));
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
        assert_eq!(endpoint1, Some(Ipv4Addr::new(5, 5, 5, 5)));
        state.register_tunnel_endpoint(client1, endpoint1.unwrap());

        // Different client can also use first endpoint (no conflict)
        let endpoint2 = state.get_available_tunnel_endpoint(client2);
        assert_eq!(endpoint2, Some(Ipv4Addr::new(5, 5, 5, 5)));
    }

    #[test]
    fn test_get_available_tunnel_endpoint_all_exhausted() {
        let device = create_test_device_with_tunnel_endpoints(vec![Ipv4Addr::new(5, 5, 5, 5)]);
        let mut state = DeviceState::new(&device);

        let client_ip = Ipv4Addr::new(2, 2, 2, 2);

        // Use the only endpoint
        let first_endpoint = state.get_available_tunnel_endpoint(client_ip).unwrap();
        state.register_tunnel_endpoint(client_ip, first_endpoint);

        // Should fall back to device public_ip
        let second_endpoint = state.get_available_tunnel_endpoint(client_ip);
        assert_eq!(second_endpoint, Some(Ipv4Addr::new(1, 1, 1, 1)));
    }

    #[test]
    fn test_get_available_tunnel_endpoint_fully_exhausted() {
        let device = create_test_device_with_tunnel_endpoints(vec![Ipv4Addr::new(5, 5, 5, 5)]);
        let mut state = DeviceState::new(&device);

        let client_ip = Ipv4Addr::new(2, 2, 2, 2);

        // Use the only dedicated endpoint
        let first_endpoint = state.get_available_tunnel_endpoint(client_ip).unwrap();
        state.register_tunnel_endpoint(client_ip, first_endpoint);

        // Use the public_ip fallback
        let second_endpoint = state.get_available_tunnel_endpoint(client_ip).unwrap();
        state.register_tunnel_endpoint(client_ip, second_endpoint);

        // Everything exhausted, should return None
        let third_endpoint = state.get_available_tunnel_endpoint(client_ip);
        assert_eq!(third_endpoint, None);
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
        assert_eq!(available, Some(endpoint));
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
        assert_eq!(available, Some(endpoint1));
    }
}
