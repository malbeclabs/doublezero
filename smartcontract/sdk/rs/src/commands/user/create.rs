use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, device::get::GetDeviceCommand,
        globalstate::get::GetGlobalStateCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_resource_extension_pda, get_user_pda},
    processors::user::create::UserCreateArgs,
    resource::ResourceType,
    state::user::{UserCYOA, UserType},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct CreateUserCommand {
    pub user_type: UserType,
    pub device_pk: Pubkey,
    pub cyoa_type: UserCYOA,
    pub client_ip: Ipv4Addr,
    pub tunnel_endpoint: Ipv4Addr,
    pub tenant_pk: Option<Pubkey>,
}

impl CreateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, _) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        // First try to get AccessPass for the client IP
        let (accesspass_pk, _) = GetAccessPassCommand {
            client_ip: self.client_ip,
            user_payer: client.get_payer(),
        }
        .execute(client)?
        .or_else(|| {
            GetAccessPassCommand {
                client_ip: Ipv4Addr::UNSPECIFIED,
                user_payer: client.get_payer(),
            }
            .execute(client)
            .ok()
            .flatten()
        })
        .ok_or_else(|| eyre::eyre!("You have no Access Pass"))?;

        let (pda_pubkey, _) =
            get_user_pda(&client.get_program_id(), &self.client_ip, self.user_type);

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: self.device_pk.to_string(),
        }
        .execute(client)
        .map_err(|_| eyre::eyre!("Device not found"))?;
        let dz_prefix_count = device.dz_prefixes.len();

        let (user_tunnel_block_ext, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::UserTunnelBlock);
        let (multicast_publisher_block_ext, _, _) = get_resource_extension_pda(
            &client.get_program_id(),
            ResourceType::MulticastPublisherBlock,
        );
        let (device_tunnel_ids_ext, _, _) = get_resource_extension_pda(
            &client.get_program_id(),
            ResourceType::TunnelIds(self.device_pk, 0),
        );

        let mut accounts = vec![
            AccountMeta::new(pda_pubkey, false),
            AccountMeta::new(self.device_pk, false),
            AccountMeta::new(accesspass_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_ext, false),
            AccountMeta::new(multicast_publisher_block_ext, false),
            AccountMeta::new(device_tunnel_ids_ext, false),
        ];
        for idx in 0..dz_prefix_count {
            let (dz_prefix_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::DzPrefixBlock(self.device_pk, idx),
            );
            accounts.push(AccountMeta::new(dz_prefix_ext, false));
        }

        // Add tenant account if provided and not default
        if let Some(tenant_pk) = self.tenant_pk {
            if tenant_pk != Pubkey::default() {
                accounts.push(AccountMeta::new(tenant_pk, false));
            }
        }

        client
            .execute_transaction(
                DoubleZeroInstruction::CreateUser(UserCreateArgs {
                    user_type: self.user_type,
                    cyoa_type: self.cyoa_type,
                    client_ip: self.client_ip,
                    tunnel_endpoint: self.tunnel_endpoint,
                    dz_prefix_count: dz_prefix_count as u8,
                }),
                accounts,
            )
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::create::CreateUserCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_accesspass_pda, get_globalstate_pda, get_resource_extension_pda, get_user_pda},
        processors::user::create::UserCreateArgs,
        resource::ResourceType,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            user::{UserCYOA, UserType},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_commands_user_create() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let (pda_pubkey, _) = get_user_pda(&program_id, &client_ip, UserType::IBRLWithAllocatedIP);

        let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: payer,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
        };
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

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
                predicate::eq(DoubleZeroInstruction::CreateUser(UserCreateArgs {
                    user_type: UserType::IBRLWithAllocatedIP,
                    cyoa_type: UserCYOA::GREOverDIA,
                    client_ip,
                    tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
                    dz_prefix_count: 1,
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(device_pk, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(user_tunnel_block_ext, false),
                    AccountMeta::new(multicast_publisher_block_ext, false),
                    AccountMeta::new(device_tunnel_ids_ext, false),
                    AccountMeta::new(dz_prefix_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CreateUserCommand {
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tenant_pk: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
