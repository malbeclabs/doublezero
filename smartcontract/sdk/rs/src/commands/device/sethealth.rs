use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::device::sethealth::DeviceSetHealthArgs,
    state::device::DeviceHealth,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SetDeviceHealthCommand {
    pub pubkey: Pubkey,
    pub health: DeviceHealth,
}

impl SetDeviceHealthCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::SetDeviceHealth(DeviceSetHealthArgs {
                health: self.health,
            }),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::device::update::UpdateDeviceCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_contributor_pda, get_globalconfig_pda},
        processors::device::update::DeviceUpdateArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            device::{Device, DeviceDesiredStatus, DeviceHealth, DeviceStatus, DeviceType},
            globalconfig::GlobalConfig,
        },
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_device_set_health_command() {
        let mut client = create_test_client();

        let (pda_pubkey, _) = get_contributor_pda(&client.get_program_id(), 1);

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test_dev".to_string(),
            contributor_pk: Pubkey::default(),
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: pda_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 250,
            users_count: 0,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Activated,
        };

        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        let (globalconfig_pubkey, _) = get_globalconfig_pda(&client.get_program_id());
        client
            .expect_get()
            .with(predicate::eq(globalconfig_pubkey))
            .returning(move |_| {
                Ok(AccountData::GlobalConfig(GlobalConfig {
                    account_type: AccountType::GlobalConfig,
                    owner: Pubkey::new_unique(),
                    bump_seed: 1,
                    local_asn: 65000,
                    remote_asn: 65001,
                    device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
                    user_tunnel_block: "10.1.0.0/24".parse().unwrap(),
                    multicastgroup_block: "224.0.0.0/24".parse().unwrap(),
                    multicast_publisher_block: "147.51.126.0/23".parse().unwrap(),
                    next_bgp_community: 1,
                }))
            });

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                    code: Some("test_device".to_string()),
                    device_type: Some(DeviceType::Hybrid),
                    public_ip: None,
                    dz_prefixes: Some("10.0.0.0/8".parse().unwrap()),
                    metrics_publisher_pk: None,
                    mgmt_vrf: Some("mgmt".to_string()),
                    contributor_pk: None,
                    max_users: None,
                    users_count: None,
                    status: None,
                    desired_status: None,
                    resource_count: 2,
                    reference_count: None,
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let update_command = UpdateDeviceCommand {
            pubkey: device_pubkey,
            code: Some("test_device".to_string()),
            contributor_pk: None,
            device_type: Some(DeviceType::Hybrid),
            public_ip: None,
            dz_prefixes: Some("10.0.0.0/8".parse().unwrap()),
            metrics_publisher: None,
            mgmt_vrf: Some("mgmt".to_string()),
            location_pk: None,
            interfaces: None,
            max_users: None,
            users_count: None,
            status: None,
            desired_status: None,
            reference_count: None,
        };

        let update_invalid = UpdateDeviceCommand {
            code: Some("test/device".to_string()),
            ..update_command.clone()
        };

        let res = update_command.execute(&client);
        assert!(res.is_ok());

        let res = update_invalid.execute(&client);
        assert!(res.is_err());
    }
}
