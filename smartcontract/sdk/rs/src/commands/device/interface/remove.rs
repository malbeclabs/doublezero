use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::device::interface::DeviceInterfaceRemoveArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct RemoveDeviceInterfaceCommand {
    pub pubkey: Pubkey,
    pub name: String,
    pub use_onchain_allocation: bool,
}

impl RemoveDeviceInterfaceCommand {
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
            DoubleZeroInstruction::RemoveDeviceInterface(DeviceInterfaceRemoveArgs {
                name: self.name.clone(),
            }),
            accounts,
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
    fn test_commands_device_interface_remove_command() {
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
        };

        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::RemoveDeviceInterface(
                    DeviceInterfaceRemoveArgs {
                        name: "Ethernet0".to_string(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = RemoveDeviceInterfaceCommand {
            pubkey: device_pubkey,
            name: "Ethernet0".to_string(),
            use_onchain_allocation: false,
        }
        .execute(&client);
        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_device_interface_remove_command_onchain() {
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
                predicate::eq(DoubleZeroInstruction::RemoveDeviceInterface(
                    DeviceInterfaceRemoveArgs {
                        name: "Ethernet0".to_string(),
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

        let res = RemoveDeviceInterfaceCommand {
            pubkey: device_pubkey,
            name: "Ethernet0".to_string(),
            use_onchain_allocation: true,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
