use crate::{idallocator::IDAllocator, ipblockallocator::IPBlockAllocator};
use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::{
    commands::device::interface::{
        activate::ActivateDeviceInterfaceCommand, reject::RejectDeviceInterfaceCommand,
        remove::RemoveDeviceInterfaceCommand, unlink::UnlinkDeviceInterfaceCommand,
    },
    CurrentInterfaceVersion, Device, DoubleZeroClient, InterfaceStatus, InterfaceType,
    LoopbackType,
};
use log::{error, info};
use solana_sdk::pubkey::Pubkey;

/// Stateless interface manager for onchain allocation mode.
/// Does not use local allocators - all allocation is handled by the smart contract.
pub struct InterfaceMgrStateless<'a> {
    client: &'a dyn DoubleZeroClient,
}

impl<'a> InterfaceMgrStateless<'a> {
    pub fn new(client: &'a dyn DoubleZeroClient) -> Self {
        Self { client }
    }

    /// Process all interfaces for a device based on their current state
    pub fn process_device_interfaces(&self, device_pubkey: &Pubkey, device: &Device) {
        for interface in device.interfaces.iter() {
            let iface = interface.into_current_version();
            self.process_interface(device_pubkey, device, iface);
        }
    }

    /// Process a single interface based on its state and type
    fn process_interface(
        &self,
        device_pubkey: &Pubkey,
        device: &Device,
        iface: CurrentInterfaceVersion,
    ) {
        match (iface.status, iface.interface_type) {
            (InterfaceStatus::Pending, InterfaceType::Loopback) => {
                info!("Event:Interface(Pending) {device_pubkey} {device:?} loopback {iface:?}");
                self.handle_pending_loopback(device_pubkey, device, &iface);
            }
            (InterfaceStatus::Pending, InterfaceType::Physical) => {
                info!("Event:Interface(Pending) {device_pubkey} {device:?} physical {iface:?}");
                self.unlink(device_pubkey, &device.code, &iface.name);
            }
            (InterfaceStatus::Pending, _) => {
                error!(
                    "Unsupported interface type {:?} for device {} interface {}",
                    iface.interface_type, device.code, iface.name
                );
            }
            (InterfaceStatus::Deleting, _) => {
                info!("Event:Interface(Deleting) {device_pubkey} {device:?} {iface:?}");
                self.handle_deleting_interface(device_pubkey, &device.code, &iface);
            }
            _ => {} // Nothing to do
        }
    }

    /// Handle a loopback interface pending activation (stateless mode)
    fn handle_pending_loopback(
        &self,
        device_pubkey: &Pubkey,
        device: &Device,
        iface: &CurrentInterfaceVersion,
    ) {
        self.activate(
            device_pubkey,
            &device.code,
            &iface.name,
            &NetworkV4::default(),
            0,
        );
    }

    /// Handle interface deletion (stateless mode - no local deallocation)
    fn handle_deleting_interface(
        &self,
        device_pubkey: &Pubkey,
        device_code: &str,
        iface: &CurrentInterfaceVersion,
    ) {
        // No local deallocation needed - onchain handles it
        self.remove(device_pubkey, device_code, &iface.name);
    }

    fn activate(
        &self,
        pubkey: &Pubkey,
        context: &str,
        name: &str,
        ip_net: &NetworkV4,
        node_segment_idx: u16,
    ) {
        let cmd = ActivateDeviceInterfaceCommand {
            pubkey: *pubkey,
            name: name.to_string(),
            ip_net: *ip_net,
            node_segment_idx,
            use_onchain_allocation: true,
        };

        if let Err(e) = cmd.execute(self.client) {
            error!("Failed to activate interface {name} on {context}: {e}");
        }
    }

    fn unlink(&self, pubkey: &Pubkey, context: &str, name: &str) {
        let cmd = UnlinkDeviceInterfaceCommand {
            pubkey: *pubkey,
            name: name.to_string(),
        };

        match cmd.execute(self.client) {
            Ok(signature) => {
                info!("Unlinked interface {name} on {context}: {signature}");
            }
            Err(e) => {
                error!("Failed to unlink interface {name} on {context}: {e}");
            }
        }
    }

    fn remove(&self, pubkey: &Pubkey, context: &str, name: &str) {
        let cmd = RemoveDeviceInterfaceCommand {
            pubkey: *pubkey,
            name: name.to_string(),
            use_onchain_allocation: true,
        };

        match cmd.execute(self.client) {
            Ok(signature) => {
                info!("Removed interface {name} on {context}: {signature}");
            }
            Err(e) => {
                error!("Failed to remove interface {name} on {context}: {e}");
            }
        }
    }
}

pub struct InterfaceMgr<'a> {
    client: &'a dyn DoubleZeroClient,
    // Optional because it's not required for process_link_event
    segment_routing_ids: Option<&'a mut IDAllocator>,
    link_ips: &'a mut IPBlockAllocator,
}

impl<'a> InterfaceMgr<'a> {
    pub fn new(
        client: &'a dyn DoubleZeroClient,
        segment_routing_ids: Option<&'a mut IDAllocator>,
        link_ips: &'a mut IPBlockAllocator,
    ) -> Self {
        Self {
            client,
            segment_routing_ids,
            link_ips,
        }
    }

    /// Process all interfaces for a device based on their current state
    pub fn process_device_interfaces(&mut self, device_pubkey: &Pubkey, device: &Device) {
        for interface in device.interfaces.iter() {
            let iface = interface.into_current_version();
            self.process_interface(device_pubkey, device, iface);
        }
    }

    /// Process a single interface based on its state and type
    fn process_interface(
        &mut self,
        device_pubkey: &Pubkey,
        device: &Device,
        mut iface: CurrentInterfaceVersion,
    ) {
        match (iface.status, iface.interface_type) {
            (InterfaceStatus::Pending, InterfaceType::Loopback) => {
                info!("Event:Interface(Pending) {device_pubkey} {device:?} loopback {iface:?}");
                self.handle_pending_loopback(device_pubkey, device, &mut iface);
            }
            (InterfaceStatus::Pending, InterfaceType::Physical) => {
                info!("Event:Interface(Pending) {device_pubkey} {device:?} physical {iface:?}");
                self.unlink(device_pubkey, &device.code, &iface.name);
            }
            (InterfaceStatus::Pending, _) => {
                error!(
                    "Unsupported interface type {:?} for device {} interface {}",
                    iface.interface_type, device.code, iface.name
                );
            }
            (InterfaceStatus::Deleting, _) => {
                info!("Event:Interface(Deleting) {device_pubkey} {device:?} {iface:?}");
                self.handle_deleting_interface(device_pubkey, &device.code, &iface);
            }
            _ => {} // Nothing to do
        }
    }

    /// Handle a loopback interface pending activation
    fn handle_pending_loopback(
        &mut self,
        device_pubkey: &Pubkey,
        device: &Device,
        iface: &mut CurrentInterfaceVersion,
    ) {
        // Allocate segment routing ID if needed
        if iface.node_segment_idx == 0 && iface.loopback_type == LoopbackType::Vpnv4 {
            if let Some(ref mut segment_routing_ids) = self.segment_routing_ids {
                iface.node_segment_idx = segment_routing_ids.next_available();
                info!(
                    "Assigning segment routing ID {} to device {} interface {}",
                    iface.node_segment_idx, device.code, iface.name
                );
            } else {
                error!(
                    "Segment routing ID allocator not available for device {} interface {}",
                    device.code, iface.name
                );
                self.reject(device_pubkey, &device.code, &iface.name);
                return;
            }
        }

        // Allocate IP if needed
        if iface.ip_net == NetworkV4::default() {
            match self.link_ips.next_available_block(1, 1) {
                Some(ip_block) => {
                    iface.ip_net = ip_block.into();
                    info!(
                        "Assigning IP {} to device {} interface {}",
                        iface.ip_net, device.code, iface.name
                    );
                }
                None => {
                    error!(
                        "No available loopback IP block for device {} interface {}",
                        device.code, iface.name
                    );
                    self.reject(device_pubkey, &device.code, &iface.name);
                    return;
                }
            }
        }

        // Activate with allocated resources
        self.activate(
            device_pubkey,
            &device.code,
            &iface.name,
            &iface.ip_net,
            iface.node_segment_idx,
        );
    }

    /// Handle interface deletion and resource cleanup
    fn handle_deleting_interface(
        &mut self,
        device_pubkey: &Pubkey,
        device_code: &str,
        iface: &CurrentInterfaceVersion,
    ) {
        // Release allocated resources
        if iface.ip_net != NetworkV4::default() {
            info!(
                "Releasing IP {} from interface {}",
                iface.ip_net, iface.name
            );
            self.link_ips.unassign_block(iface.ip_net.into());
        }

        if iface.node_segment_idx != 0 {
            if let Some(ref mut segment_routing_ids) = self.segment_routing_ids {
                info!(
                    "Releasing segment routing ID {} from interface {}",
                    iface.node_segment_idx, iface.name
                );
                segment_routing_ids.unassign(iface.node_segment_idx);
            }
        }

        self.remove(device_pubkey, device_code, &iface.name);
    }

    fn activate(
        &self,
        pubkey: &Pubkey,
        context: &str,
        name: &str,
        ip_net: &NetworkV4,
        node_segment_idx: u16,
    ) {
        let cmd = ActivateDeviceInterfaceCommand {
            pubkey: *pubkey,
            name: name.to_string(),
            ip_net: *ip_net,
            node_segment_idx,
            use_onchain_allocation: false,
        };

        if let Err(e) = cmd.execute(self.client) {
            error!("Failed to activate interface {name} on {context}: {e}");
        }
    }

    fn unlink(&self, pubkey: &Pubkey, context: &str, name: &str) {
        let cmd = UnlinkDeviceInterfaceCommand {
            pubkey: *pubkey,
            name: name.to_string(),
        };

        match cmd.execute(self.client) {
            Ok(signature) => {
                info!("Unlinked interface {name} on {context}: {signature}");
            }
            Err(e) => {
                error!("Failed to unlink interface {name} on {context}: {e}");
            }
        }
    }

    fn reject(&self, pubkey: &Pubkey, context: &str, name: &str) {
        let cmd = RejectDeviceInterfaceCommand {
            pubkey: *pubkey,
            name: name.to_string(),
        };

        match cmd.execute(self.client) {
            Ok(signature) => {
                info!("Rejected interface {name} on {context}: {signature}");
            }
            Err(e) => {
                error!("Failed to reject interface {name} on {context}: {e}");
            }
        }
    }

    fn remove(&self, pubkey: &Pubkey, context: &str, name: &str) {
        let cmd = RemoveDeviceInterfaceCommand {
            pubkey: *pubkey,
            name: name.to_string(),
            use_onchain_allocation: false,
        };

        match cmd.execute(self.client) {
            Ok(signature) => {
                info!("Removed interface {name} on {context}: {signature}");
            }
            Err(e) => {
                error!("Failed to remove interface {name} on {context}: {e}");
            }
        }
    }
}
