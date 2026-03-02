use crate::{
    commands::{device::get::GetDeviceCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::device::interface::DeviceInterfaceUpdateArgs,
    state::interface::{InterfaceCYOA, InterfaceDIA, InterfaceStatus, LoopbackType, RoutingMode},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

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
}

impl UpdateDeviceInterfaceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (device_pubkey, device) = GetDeviceCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)?;

        client.execute_transaction(
            DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
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
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(device.contributor_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_serviceability::{
        pda::get_globalstate_pda,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            device::{Device, DeviceDesiredStatus, DeviceHealth, DeviceStatus, DeviceType},
        },
    };
    use mockall::predicate;

    #[test]
    fn test_commands_device_interface_update_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());

        let device_pubkey = Pubkey::new_unique();

        let device = Device {
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
            multicast_users_count: 0,
            max_unicast_users: 0,
            max_multicast_users: 0,
            reserved_seats: 0,
        };

        let contributor_pk = device.contributor_pk;

        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateDeviceInterface(
                    DeviceInterfaceUpdateArgs {
                        name: "Ethernet0".to_string(),
                        loopback_type: None,
                        bandwidth: None,
                        cir: None,
                        mtu: None,
                        routing_mode: None,
                        interface_dia: None,
                        vlan_id: Some(42),
                        user_tunnel_endpoint: None,
                        interface_cyoa: None,
                        status: None,
                        ip_net: None,
                        node_segment_idx: None,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(contributor_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

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
        };

        let res = update_command.execute(&client);
        assert!(res.is_ok());
    }
}
