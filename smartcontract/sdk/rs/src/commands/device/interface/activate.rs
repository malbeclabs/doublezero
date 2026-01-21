use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::device::interface::DeviceInterfaceActivateArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct ActivateDeviceInterfaceCommand {
    pub pubkey: Pubkey,
    pub name: String,
    pub ip_net: NetworkV4,
    pub node_segment_idx: u16,
    pub use_onchain_allocation: bool,
}

impl ActivateDeviceInterfaceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if self.use_onchain_allocation {
            let (link_ips_pda, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::DeviceTunnelBlock,
            );
            let (segment_routing_id_pda, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::SegmentRoutingIds,
            );
            accounts.push(AccountMeta::new(link_ips_pda, false));
            accounts.push(AccountMeta::new(segment_routing_id_pda, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::ActivateDeviceInterface(DeviceInterfaceActivateArgs {
                name: self.name.clone(),
                ip_net: self.ip_net,
                node_segment_idx: self.node_segment_idx,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_serviceability::pda::get_globalstate_pda;
    use mockall::predicate;

    #[test]
    fn test_commands_device_interface_activate_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());

        let device_pubkey = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateDeviceInterface(
                    DeviceInterfaceActivateArgs {
                        name: "Ethernet0".to_string(),
                        ip_net: "10.0.0.0/31".parse().unwrap(),
                        node_segment_idx: 1,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ActivateDeviceInterfaceCommand {
            pubkey: device_pubkey,
            name: "Ethernet0".to_string(),
            ip_net: "10.0.0.0/31".parse().unwrap(),
            node_segment_idx: 1,
            use_onchain_allocation: false,
        }
        .execute(&client);
        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_device_interface_activate_command_onchain() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (link_ips_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::DeviceTunnelBlock);
        let (segment_routing_id_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::SegmentRoutingIds);

        let device_pubkey = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateDeviceInterface(
                    DeviceInterfaceActivateArgs {
                        name: "Ethernet0".to_string(),
                        ip_net: "10.0.0.0/31".parse().unwrap(),
                        node_segment_idx: 1,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(link_ips_pda, false),
                    AccountMeta::new(segment_routing_id_pda, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ActivateDeviceInterfaceCommand {
            pubkey: device_pubkey,
            name: "Ethernet0".to_string(),
            ip_net: "10.0.0.0/31".parse().unwrap(),
            node_segment_idx: 1,
            use_onchain_allocation: true,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
