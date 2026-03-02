use crate::{
    commands::{device::get::GetDeviceCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};
use doublezero_program_common::types::network_v4::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::device::interface::create::DeviceInterfaceCreateArgs,
    state::interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

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
}

impl CreateDeviceInterfaceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, _) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (device_pubkey, device) = GetDeviceCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)?;

        client
            .execute_transaction(
                DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
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
                }),
                vec![
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(device.contributor_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, device_pubkey))
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
    fn test_commands_device_create_interface_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());

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
                predicate::eq(DoubleZeroInstruction::CreateDeviceInterface(
                    DeviceInterfaceCreateArgs {
                        name: "Ethernet0".to_string(),
                        loopback_type: LoopbackType::None,
                        interface_cyoa: InterfaceCYOA::None,
                        interface_dia: InterfaceDIA::DIA,
                        bandwidth: 0,
                        cir: 0,
                        ip_net: None,

                        mtu: 1500,
                        routing_mode: RoutingMode::Static,
                        vlan_id: 100,
                        user_tunnel_endpoint: true,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(contributor_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = CreateDeviceInterfaceCommand {
            pubkey: device_pubkey,
            name: "Ethernet0".to_string(),
            interface_cyoa: InterfaceCYOA::None,
            loopback_type: LoopbackType::None,
            interface_dia: InterfaceDIA::DIA,
            ip_net: None,
            bandwidth: 0,
            cir: 0,
            mtu: 1500,
            routing_mode: RoutingMode::Static,
            vlan_id: 100,
            user_tunnel_endpoint: true,
        };

        let res = command.execute(&client);
        assert!(res.is_ok());
    }
}
