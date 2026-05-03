use crate::{
    commands::{
        device::get::GetDeviceCommand, globalstate::get::GetGlobalStateCommand,
        user::get::GetUserCommand,
    },
    DoubleZeroClient,
};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_resource_extension_pda,
    processors::user::update::UserUpdateArgs,
    resource::ResourceType,
    state::user::{UserCYOA, UserType},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateUserCommand {
    pub pubkey: Pubkey,
    pub user_type: Option<UserType>,
    pub cyoa_type: Option<UserCYOA>,
    pub dz_ip: Option<Ipv4Addr>,
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
    pub validator_pubkey: Option<Pubkey>,
    pub tenant_pk: Option<Pubkey>,
    pub tunnel_endpoint: Option<Ipv4Addr>,
}

impl UpdateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let updating_resources =
            self.dz_ip.is_some() || self.tunnel_id.is_some() || self.tunnel_net.is_some();

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        let (dz_prefix_count, multicast_publisher_count) = if updating_resources {
            let (_user_pubkey, user) = GetUserCommand {
                pubkey: self.pubkey,
            }
            .execute(client)?;

            let (_, device) = GetDeviceCommand {
                pubkey_or_code: user.device_pk.to_string(),
            }
            .execute(client)
            .map_err(|_| eyre::eyre!("Device not found"))?;

            let count = device.dz_prefixes.len();

            let (user_tunnel_block_ext, _, _) =
                get_resource_extension_pda(&client.get_program_id(), ResourceType::UserTunnelBlock);
            accounts.push(AccountMeta::new(user_tunnel_block_ext, false));

            let (multicast_publisher_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::MulticastPublisherBlock,
            );
            accounts.push(AccountMeta::new(multicast_publisher_block_ext, false));

            let (device_tunnel_ids_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::TunnelIds(user.device_pk, 0),
            );
            accounts.push(AccountMeta::new(device_tunnel_ids_ext, false));

            for idx in 0..count {
                let (dz_prefix_ext, _, _) = get_resource_extension_pda(
                    &client.get_program_id(),
                    ResourceType::DzPrefixBlock(user.device_pk, idx),
                );
                accounts.push(AccountMeta::new(dz_prefix_ext, false));
            }

            (count as u8, 1u8)
        } else {
            (0u8, 0u8)
        };

        // If updating tenant_pk, add old and new tenant accounts for reference counting
        if let Some(new_tenant_pk) = self.tenant_pk {
            // Get current user to find old tenant (may already have been fetched above)
            let (_user_pubkey, user) = GetUserCommand {
                pubkey: self.pubkey,
            }
            .execute(client)?;

            let old_tenant_pk = user.tenant_pk;

            // Add tenant accounts (old_tenant, new_tenant)
            accounts.push(AccountMeta::new(old_tenant_pk, false));
            accounts.push(AccountMeta::new(new_tenant_pk, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
                user_type: self.user_type,
                cyoa_type: self.cyoa_type,
                dz_ip: self.dz_ip,
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
                validator_pubkey: self.validator_pubkey,
                tenant_pk: self.tenant_pk,
                dz_prefix_count,
                multicast_publisher_count,
                tunnel_endpoint: self.tunnel_endpoint,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::update::UpdateUserCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_resource_extension_pda},
        processors::user::update::UserUpdateArgs,
        resource::ResourceType,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_commands_user_update_with_resource_fields() {
        let mut client = create_test_client();

        let payer = client.get_payer();
        let program_id = client.get_program_id();
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let user = User {
            account_type: AccountType::User,
            owner: payer,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
        };

        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        let (user_tunnel_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
        let (multicast_publisher_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
        let (device_tunnel_ids_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pk, 0));
        let (dz_prefix_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pk, 0));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
                    user_type: None,
                    cyoa_type: None,
                    dz_ip: None,
                    tunnel_id: Some(501),
                    tunnel_net: None,
                    validator_pubkey: None,
                    tenant_pk: None,
                    dz_prefix_count: 1,
                    multicast_publisher_count: 1,
                    tunnel_endpoint: None,
                })),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(user_tunnel_block_ext, false),
                    AccountMeta::new(multicast_publisher_block_ext, false),
                    AccountMeta::new(device_tunnel_ids_ext, false),
                    AccountMeta::new(dz_prefix_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = UpdateUserCommand {
            pubkey: user_pubkey,
            user_type: None,
            cyoa_type: None,
            dz_ip: None,
            tunnel_id: Some(501),
            tunnel_net: None,
            validator_pubkey: None,
            tenant_pk: None,
            tunnel_endpoint: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_user_update_no_resource_fields_skips_accounts() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let user_pubkey = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
                    user_type: Some(UserType::IBRL),
                    cyoa_type: None,
                    dz_ip: None,
                    tunnel_id: None,
                    tunnel_net: None,
                    validator_pubkey: None,
                    tenant_pk: None,
                    dz_prefix_count: 0,
                    multicast_publisher_count: 0,
                    tunnel_endpoint: None,
                })),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = UpdateUserCommand {
            pubkey: user_pubkey,
            user_type: Some(UserType::IBRL),
            cyoa_type: None,
            dz_ip: None,
            tunnel_id: None,
            tunnel_net: None,
            validator_pubkey: None,
            tenant_pk: None,
            tunnel_endpoint: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
