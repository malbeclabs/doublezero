use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::device::activate::DeviceActivateArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{
    commands::{
        device::get::GetDeviceCommand, globalconfig::get::GetGlobalConfigCommand,
        globalstate::get::GetGlobalStateCommand,
    },
    DoubleZeroClient,
};

#[derive(Debug, PartialEq, Clone)]
pub struct ActivateDeviceCommand {
    pub device_pubkey: Pubkey,
}

impl ActivateDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (globalconfig_pubkey, _globalconfig) = GetGlobalConfigCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalconfig not initialized"))?;

        let (_device_pubkey, device) = GetDeviceCommand {
            pubkey_or_code: self.device_pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Device not found"))?;

        let mut extra_accounts = vec![];
        for idx in 0..device.dz_prefixes.len() + 1 {
            let resource_type = match idx {
                0 => ResourceType::TunnelIds(self.device_pubkey, 0),
                _ => ResourceType::DzPrefixBlock(self.device_pubkey, idx - 1),
            };
            let (pda, _, _) = get_resource_extension_pda(&client.get_program_id(), resource_type);
            extra_accounts.push(AccountMeta::new(pda, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs {
                resource_count: extra_accounts.len(),
            }),
            [
                vec![
                    AccountMeta::new(self.device_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(globalconfig_pubkey, false),
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
        commands::device::activate::ActivateDeviceCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{
            get_device_pda, get_globalconfig_pda, get_globalstate_pda, get_resource_extension_pda,
        },
        processors::device::activate::DeviceActivateArgs,
        resource::ResourceType,
        state::{
            accountdata::AccountData, accounttype::AccountType, device::Device,
            globalconfig::GlobalConfig,
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_device_activate_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (globalconfig_pubkey, _) = get_globalconfig_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_device_pda(&client.get_program_id(), 1);
        let (tunnel_ids_pda, _, _) = get_resource_extension_pda(
            &client.get_program_id(),
            ResourceType::TunnelIds(pda_pubkey, 0),
        );
        let (dz_prefix0_pda, _, _) = get_resource_extension_pda(
            &client.get_program_id(),
            ResourceType::DzPrefixBlock(pda_pubkey, 0),
        );

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
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| {
                Ok(AccountData::Device(Device {
                    dz_prefixes: "3.0.0.0/24".parse().unwrap(),
                    ..Device::default()
                }))
            });

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs {
                    resource_count: 2,
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(globalconfig_pubkey, false),
                    AccountMeta::new(tunnel_ids_pda, false),
                    AccountMeta::new(dz_prefix0_pda, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ActivateDeviceCommand {
            device_pubkey: pda_pubkey,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
