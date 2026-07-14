use crate::{commands::device::get::GetDeviceCommand, DoubleZeroClient};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    processors::device::interface::DeviceInterfaceUpdateArgs,
    state::interface::{InterfaceCYOA, InterfaceDIA, InterfaceStatus, LoopbackType, RoutingMode},
};
use doublezero_serviceability_instruction::device::update_device_interface;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateDeviceInterfaceCommand {
    pub pubkey: Pubkey,
    pub name: String,
    pub loopback_type: Option<LoopbackType>,
    pub interface_cyoa: Option<InterfaceCYOA>,
    pub interface_dia: Option<InterfaceDIA>,
    pub bandwidth: Option<u64>,
    pub cir: Option<u64>,
    pub mtu: Option<u16>,
    pub routing_mode: Option<RoutingMode>,
    pub vlan_id: Option<u16>,
    pub user_tunnel_endpoint: Option<bool>,
    pub status: Option<InterfaceStatus>,
    pub ip_net: Option<NetworkV4>,
    pub node_segment_idx: Option<u16>,
    /// Reconcile flex-algo topology set on a Vpnv4 loopback. None leaves it
    /// alone; Some(vec![]) clears all entries; Some(names) sets exactly that
    /// set. Names are resolved to Topology PDAs via get_topology_pda.
    pub topology_names: Option<Vec<String>>,
}

impl UpdateDeviceInterfaceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (device_pubkey, device) = GetDeviceCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)?;

        // The builder appends the SegmentRoutingIds resource (when node_segment_idx
        // or topologies change) and the topology PDAs, and writes update_topologies /
        // topology_count from the topology_names choice.
        client.send_transaction(update_device_interface(
            &client.get_program_id(),
            &client.get_payer(),
            &device_pubkey,
            &device.contributor_pk,
            self.topology_names.as_deref(),
            DeviceInterfaceUpdateArgs {
                name: self.name.clone(),
                loopback_type: self.loopback_type,
                interface_cyoa: self.interface_cyoa,
                interface_dia: self.interface_dia,
                bandwidth: self.bandwidth,
                cir: self.cir,
                mtu: self.mtu,
                routing_mode: self.routing_mode,
                vlan_id: self.vlan_id,
                user_tunnel_endpoint: self.user_tunnel_endpoint,
                status: self.status,
                ip_net: self.ip_net,
                node_segment_idx: self.node_segment_idx,
                topology_count: 0,
                update_topologies: false,
            },
        ))
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
    fn test_commands_device_interface_update_command() {
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

        let expected = update_device_interface(
            &program_id,
            &payer,
            &device_pubkey,
            &contributor_pk,
            None,
            DeviceInterfaceUpdateArgs {
                name: "Ethernet0".to_string(),
                vlan_id: Some(42),
                ..Default::default()
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let update_command = UpdateDeviceInterfaceCommand {
            pubkey: device_pubkey,
            name: "Ethernet0".to_string(),
            loopback_type: None,
            interface_cyoa: None,
            bandwidth: None,
            cir: None,
            mtu: None,
            routing_mode: None,
            vlan_id: Some(42),
            user_tunnel_endpoint: None,
            status: None,
            ip_net: None,
            interface_dia: None,
            node_segment_idx: None,
            topology_names: None,
        };

        let res = update_command.execute(&client);
        assert!(res.is_ok());
    }

    /// node_segment_idx update includes the SegmentRoutingIds resource account.
    #[test]
    fn test_commands_device_interface_update_node_segment_idx() {
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

        let expected = update_device_interface(
            &program_id,
            &payer,
            &device_pubkey,
            &contributor_pk,
            None,
            DeviceInterfaceUpdateArgs {
                name: "loopback0".to_string(),
                node_segment_idx: Some(42),
                ..Default::default()
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let update_command = UpdateDeviceInterfaceCommand {
            pubkey: device_pubkey,
            name: "loopback0".to_string(),
            node_segment_idx: Some(42),
            loopback_type: None,
            interface_cyoa: None,
            interface_dia: None,
            bandwidth: None,
            cir: None,
            mtu: None,
            routing_mode: None,
            vlan_id: None,
            user_tunnel_endpoint: None,
            status: None,
            ip_net: None,
            topology_names: None,
        };

        let res = update_command.execute(&client);
        assert!(res.is_ok());
    }

    /// Test that updating topologies appends seg_routing + topology PDAs and sets the flags
    #[test]
    fn test_commands_device_interface_update_topologies() {
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

        // The builder writes update_topologies=true / topology_count=2 and appends
        // the SegmentRoutingIds resource and the two topology PDAs.
        let expected = update_device_interface(
            &program_id,
            &payer,
            &device_pubkey,
            &contributor_pk,
            Some(&["TOPO-A".to_string(), "TOPO-B".to_string()]),
            DeviceInterfaceUpdateArgs {
                name: "Loopback256".to_string(),
                ..Default::default()
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = UpdateDeviceInterfaceCommand {
            pubkey: device_pubkey,
            name: "Loopback256".to_string(),
            loopback_type: None,
            interface_cyoa: None,
            interface_dia: None,
            bandwidth: None,
            cir: None,
            mtu: None,
            routing_mode: None,
            vlan_id: None,
            user_tunnel_endpoint: None,
            status: None,
            ip_net: None,
            node_segment_idx: None,
            topology_names: Some(vec!["TOPO-A".to_string(), "TOPO-B".to_string()]),
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
