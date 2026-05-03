use crate::{
    commands::{device::get::GetDeviceCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::device::interface::DeviceInterfaceDeleteArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteDeviceInterfaceCommand {
    pub pubkey: Pubkey,
    pub name: String,
}

impl DeleteDeviceInterfaceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (device_pubkey, device) = GetDeviceCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)?;

        let (device_tunnel_block_ext, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::DeviceTunnelBlock);
        let (segment_routing_ids_ext, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::SegmentRoutingIds);
        let accounts = vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(device.contributor_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_ext, false),
            AccountMeta::new(segment_routing_ids_ext, false),
        ];

        client.execute_transaction(
            DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
                name: self.name.clone(),
                use_onchain_deallocation: true,
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
        }
    }

    #[test]
    fn test_commands_device_interface_delete() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let (device_tunnel_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
        let (segment_routing_ids_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

        let device_pubkey = Pubkey::new_unique();
        let device = make_test_device();
        let contributor_pk = device.contributor_pk;

        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteDeviceInterface(
                    DeviceInterfaceDeleteArgs {
                        name: "Loopback0".to_string(),
                        use_onchain_deallocation: true,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(contributor_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(device_tunnel_block_ext, false),
                    AccountMeta::new(segment_routing_ids_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteDeviceInterfaceCommand {
            pubkey: device_pubkey,
            name: "Loopback0".to_string(),
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
