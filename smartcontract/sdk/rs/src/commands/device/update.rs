use crate::{
    commands::{
        device::get::GetDeviceCommand, globalconfig::get::GetGlobalConfigCommand,
        globalstate::get::GetGlobalStateCommand,
    },
    DoubleZeroClient,
};
use doublezero_program_common::{types::NetworkV4List, validate_account_code};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_resource_extension_pda,
    processors::device::update::DeviceUpdateArgs,
    resource::ResourceType,
    state::{
        device::{DeviceDesiredStatus, DeviceStatus, DeviceType},
        interface::Interface,
    },
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateDeviceCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub device_type: Option<DeviceType>,
    pub public_ip: Option<Ipv4Addr>,
    pub dz_prefixes: Option<NetworkV4List>,
    pub metrics_publisher: Option<Pubkey>,
    pub contributor_pk: Option<Pubkey>,
    pub location_pk: Option<Pubkey>,
    pub mgmt_vrf: Option<String>,
    pub interfaces: Option<Vec<Interface>>,
    pub max_users: Option<u16>,
    pub users_count: Option<u16>,
    pub status: Option<DeviceStatus>,
    pub desired_status: Option<DeviceDesiredStatus>,
    pub reference_count: Option<u32>,
    pub max_unicast_users: Option<u16>,
    pub max_multicast_users: Option<u16>,
}

impl UpdateDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let code = self
            .code
            .as_ref()
            .map(|code| {
                validate_account_code(code).map(|mut c| {
                    c.make_ascii_lowercase();
                    c
                })
            })
            .transpose()
            .map_err(|err| eyre::eyre!("invalid code: {err}"))?;
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (globalconfig_pubkey, _globalconfig) = GetGlobalConfigCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalconfig not initialized"))?;

        let (device_pubkey, device) = GetDeviceCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Device not found"))?;

        let mut extra_accounts = vec![];
        let mut resource_count = 0;
        if let Some(dz_prefixes) = &self.dz_prefixes {
            extra_accounts.push(AccountMeta::new(globalconfig_pubkey, false));
            let old_count = device.dz_prefixes.len();
            let new_count = dz_prefixes.len();
            let max_count = old_count.max(new_count);
            for idx in 0..max_count + 1 {
                let resource_type = match idx {
                    0 => ResourceType::TunnelIds(device_pubkey, 0),
                    _ => ResourceType::DzPrefixBlock(device_pubkey, idx - 1),
                };
                let (pda, _, _) =
                    get_resource_extension_pda(&client.get_program_id(), resource_type);
                extra_accounts.push(AccountMeta::new(pda, false));
            }
            resource_count += max_count + 1;
        }

        client.execute_transaction(
            DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                code,
                contributor_pk: self.contributor_pk,
                device_type: self.device_type,
                public_ip: self.public_ip,
                dz_prefixes: self.dz_prefixes.clone(),
                metrics_publisher_pk: self.metrics_publisher,
                mgmt_vrf: self.mgmt_vrf.clone(),
                max_users: self.max_users,
                users_count: self.users_count,
                status: self.status,
                desired_status: self.desired_status,
                resource_count,
                reference_count: self.reference_count,
                max_unicast_users: self.max_unicast_users,
                max_multicast_users: self.max_multicast_users,
            }),
            [
                vec![
                    AccountMeta::new(self.pubkey, false),
                    AccountMeta::new(device.contributor_pk, false),
                    AccountMeta::new(device.location_pk, false),
                    AccountMeta::new(self.location_pk.unwrap_or(device.location_pk), false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
                extra_accounts,
            ]
            .concat(),
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
    fn test_commands_device_update_command() {
        let mut client = create_test_client();

        let (pda_pubkey, _) = get_contributor_pda(&client.get_program_id(), 1);
        let (globalconfig_pubkey, _) = get_globalconfig_pda(&client.get_program_id());

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
            unicast_users_count: 0,
            multicast_users_count: 0,
            max_unicast_users: 0,
            max_multicast_users: 0,
        };

        client
            .expect_get()
            .with(predicate::eq(globalconfig_pubkey))
            .returning(move |_| {
                Ok(AccountData::GlobalConfig(GlobalConfig {
                    account_type: AccountType::GlobalConfig,
                    owner: Pubkey::default(),
                    bump_seed: 0,
                    local_asn: 0,
                    remote_asn: 0,
                    device_tunnel_block: "1.0.0.0/24".parse().unwrap(),
                    user_tunnel_block: "2.0.0.0/24".parse().unwrap(),
                    multicastgroup_block: "224.0.0.0/24".parse().unwrap(),
                    multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
                    next_bgp_community: 0,
                }))
            });

        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device.clone())));
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
                    max_unicast_users: None,
                    max_multicast_users: None,
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
            max_unicast_users: None,
            max_multicast_users: None,
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
