use crate::{commands::device::get::GetDeviceCommand, DoubleZeroClient};
use doublezero_program_common::types::network_v4::NetworkV4;
use doublezero_serviceability::{
    processors::device::interface::create::DeviceInterfaceCreateArgs,
    state::interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
};
use doublezero_serviceability_instruction::device::create_device_interface;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateDeviceInterfaceCommand {
    pub pubkey: Pubkey,
    pub name: String,
    pub ip_net: Option<NetworkV4>,
    pub interface_cyoa: InterfaceCYOA,
    pub loopback_type: LoopbackType,
    pub interface_dia: InterfaceDIA,
    pub bandwidth: u64,
    pub cir: u64,
    pub mtu: u16,
    pub routing_mode: RoutingMode,
    pub vlan_id: u16,
    pub user_tunnel_endpoint: bool,
    /// Topology names to assign flex-algo node segments for (Vpnv4 loopbacks only).
    /// Empty means no topology assignment.
    pub topology_names: Vec<String>,
}

impl CreateDeviceInterfaceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (device_pubkey, device) = GetDeviceCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)?;

        // The builder appends topology PDAs (and writes topology_count) only for
        // Vpnv4 loopbacks; topology_names is ignored otherwise.
        let ix = create_device_interface(
            &client.get_program_id(),
            &client.get_payer(),
            &device_pubkey,
            &device.contributor_pk,
            &self.topology_names,
            DeviceInterfaceCreateArgs {
                name: self.name.clone(),
                loopback_type: self.loopback_type,
                interface_cyoa: self.interface_cyoa,
                interface_dia: self.interface_dia,
                bandwidth: self.bandwidth,
                cir: self.cir,
                ip_net: self.ip_net,
                mtu: self.mtu,
                routing_mode: self.routing_mode,
                vlan_id: self.vlan_id,
                user_tunnel_endpoint: self.user_tunnel_endpoint,
                use_onchain_allocation: true,
                topology_count: 0,
            },
        );

        client.send_transaction(ix).map(|sig| (sig, device_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        device::{Device, DeviceDesiredStatus, DeviceHealth, DeviceStatus, DeviceType},
    };
    use mockall::predicate;

    fn make_test_device() -> Device {
        Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            reference_count: 0,
            bump_seed: 0,
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Hybrid,
            public_ip: [192, 168, 1, 2].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
            ..Default::default()
        }
    }

    #[test]
    fn test_commands_device_create_interface() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let device_pubkey = Pubkey::new_unique();
        let device = make_test_device();
        let contributor_pk = device.contributor_pk;

        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        let expected = create_device_interface(
            &program_id,
            &payer,
            &device_pubkey,
            &contributor_pk,
            &[],
            DeviceInterfaceCreateArgs {
                name: "Loopback0".to_string(),
                loopback_type: LoopbackType::Vpnv4,
                interface_cyoa: InterfaceCYOA::None,
                interface_dia: InterfaceDIA::None,
                bandwidth: 0,
                cir: 0,
                ip_net: None,
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
                use_onchain_allocation: true,
                topology_count: 0,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = CreateDeviceInterfaceCommand {
            pubkey: device_pubkey,
            name: "Loopback0".to_string(),
            loopback_type: LoopbackType::Vpnv4,
            interface_cyoa: InterfaceCYOA::None,
            interface_dia: InterfaceDIA::None,
            ip_net: None,
            bandwidth: 0,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            topology_names: vec![],
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
