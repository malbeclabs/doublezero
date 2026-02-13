use crate::{
    commands::{
        device::get::GetDeviceCommand, globalconfig::get::GetGlobalConfigCommand,
        globalstate::get::GetGlobalStateCommand, user::get::GetUserCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::user::closeaccount::UserCloseAccountArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct CloseAccountUserCommand {
    pub pubkey: Pubkey,
    /// When true, SDK computes ResourceExtension PDAs and includes them for on-chain deallocation.
    /// When false, uses legacy behavior without resource deallocation.
    pub use_onchain_deallocation: bool,
}

impl CloseAccountUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, user) = GetUserCommand {
            pubkey: self.pubkey,
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("User not found"))?;

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(user.owner, false),
            AccountMeta::new(user.device_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        let (dz_prefix_count, multicast_publisher_count): (u8, u8) = if self
            .use_onchain_deallocation
        {
            // Fetch device to get dz_prefixes count
            let (_, device) = GetDeviceCommand {
                pubkey_or_code: user.device_pk.to_string(),
            }
            .execute(client)
            .map_err(|_| eyre::eyre!("Device not found"))?;

            let count = device.dz_prefixes.len();

            // Global UserTunnelBlock
            let (global_resource_ext, _, _) =
                get_resource_extension_pda(&client.get_program_id(), ResourceType::UserTunnelBlock);

            accounts.push(AccountMeta::new(global_resource_ext, false));

            // Determine if user has MulticastPublisherBlock allocation
            // Check if dz_ip is allocated and could be from MulticastPublisherBlock
            let needs_multicast_publisher_block =
                user.dz_ip != user.client_ip && user.dz_ip != Ipv4Addr::UNSPECIFIED && {
                    // Fetch GlobalConfig to check if dz_ip is in multicast_publisher_block range
                    let (_, globalconfig) = GetGlobalConfigCommand
                        .execute(client)
                        .map_err(|_| eyre::eyre!("GlobalConfig not initialized"))?;
                    globalconfig.multicast_publisher_block.contains(user.dz_ip)
                };

            let multicast_count = if needs_multicast_publisher_block {
                let (multicast_publisher_block_ext, _, _) = get_resource_extension_pda(
                    &client.get_program_id(),
                    ResourceType::MulticastPublisherBlock,
                );
                accounts.push(AccountMeta::new(multicast_publisher_block_ext, false));
                1
            } else {
                0
            };

            // Device TunnelIds (scoped to user's device)
            let (device_tunnel_ids_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::TunnelIds(user.device_pk, 0),
            );

            accounts.push(AccountMeta::new(device_tunnel_ids_ext, false));

            // Add all N DzPrefixBlock accounts (devices can have multiple dz_prefixes)
            for idx in 0..count {
                let (device_dz_prefix_ext, _, _) = get_resource_extension_pda(
                    &client.get_program_id(),
                    ResourceType::DzPrefixBlock(user.device_pk, idx),
                );
                accounts.push(AccountMeta::new(device_dz_prefix_ext, false));
            }

            (count as u8, multicast_count)
        } else {
            (0, 0)
        };

        if user.tenant_pk != Pubkey::default() {
            accounts.push(AccountMeta::new(user.tenant_pk, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {
                dz_prefix_count,
                multicast_publisher_count,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::closeaccount::CloseAccountUserCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalconfig_pda, get_globalstate_pda, get_resource_extension_pda},
        processors::user::closeaccount::UserCloseAccountArgs,
        resource::ResourceType,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            globalconfig::GlobalConfig,
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_commands_user_closeaccount_without_resource_extension() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let owner = client.get_payer();

        let user = User {
            account_type: AccountType::User,
            owner,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: Ipv4Addr::new(192, 168, 1, 10),
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: "10.0.0.0/24".parse().unwrap(),
            status: UserStatus::Deleting,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        // Mock User fetch
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CloseAccountUser(
                    UserCloseAccountArgs {
                        dz_prefix_count: 0,
                        multicast_publisher_count: 0,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(owner, false),
                    AccountMeta::new(device_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CloseAccountUserCommand {
            pubkey: user_pubkey,
            use_onchain_deallocation: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_user_closeaccount_with_onchain_deallocation() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let owner = client.get_payer();

        let user = User {
            account_type: AccountType::User,
            owner,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: Ipv4Addr::new(192, 168, 1, 10),
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: "10.0.0.0/24".parse().unwrap(),
            status: UserStatus::Deleting,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        // Compute ResourceExtension PDAs
        let (global_resource_ext, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::UserTunnelBlock);
        let (device_tunnel_ids_ext, _, _) = get_resource_extension_pda(
            &client.get_program_id(),
            ResourceType::TunnelIds(device_pk, 0),
        );
        let (device_dz_prefix_ext, _, _) = get_resource_extension_pda(
            &client.get_program_id(),
            ResourceType::DzPrefixBlock(device_pk, 0),
        );

        // Mock User fetch
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        // Mock Device fetch (for dz_prefixes.len())
        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        // Mock GlobalConfig fetch (to check if dz_ip is in multicast_publisher_block)
        let (globalconfig_pubkey, bump_seed) = get_globalconfig_pda(&client.get_program_id());
        let globalconfig = GlobalConfig {
            account_type: AccountType::GlobalConfig,
            owner: Pubkey::default(),
            bump_seed,
            local_asn: 0,
            remote_asn: 0,
            device_tunnel_block: "1.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "2.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/24".parse().unwrap(),
            multicast_publisher_block: "147.51.126.0/23".parse().unwrap(), // dz_ip 10.0.0.1 NOT in this range
            next_bgp_community: 0,
        };
        client
            .expect_get()
            .with(predicate::eq(globalconfig_pubkey))
            .returning(move |_| Ok(AccountData::GlobalConfig(globalconfig.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CloseAccountUser(
                    UserCloseAccountArgs {
                        dz_prefix_count: 1, // 1 dz_prefix from device.dz_prefixes
                        multicast_publisher_count: 0,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(owner, false),
                    AccountMeta::new(device_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(global_resource_ext, false),
                    AccountMeta::new(device_tunnel_ids_ext, false),
                    AccountMeta::new(device_dz_prefix_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CloseAccountUserCommand {
            pubkey: user_pubkey,
            use_onchain_deallocation: true,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_user_closeaccount_with_tenant() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let tenant_pk = Pubkey::new_unique();
        let owner = client.get_payer();

        let user = User {
            account_type: AccountType::User,
            owner,
            bump_seed: 0,
            index: 1,
            tenant_pk,
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: Ipv4Addr::new(192, 168, 1, 10),
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: "10.0.0.0/24".parse().unwrap(),
            status: UserStatus::Deleting,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CloseAccountUser(
                    UserCloseAccountArgs {
                        dz_prefix_count: 0,
                        multicast_publisher_count: 0,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(owner, false),
                    AccountMeta::new(device_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(tenant_pk, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CloseAccountUserCommand {
            pubkey: user_pubkey,
            use_onchain_deallocation: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_user_closeaccount_with_tenant_and_onchain_deallocation() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let tenant_pk = Pubkey::new_unique();
        let owner = client.get_payer();

        let user = User {
            account_type: AccountType::User,
            owner,
            bump_seed: 0,
            index: 1,
            tenant_pk,
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: Ipv4Addr::new(192, 168, 1, 10),
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: "10.0.0.0/24".parse().unwrap(),
            status: UserStatus::Deleting,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        let (global_resource_ext, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::UserTunnelBlock);
        let (device_tunnel_ids_ext, _, _) = get_resource_extension_pda(
            &client.get_program_id(),
            ResourceType::TunnelIds(device_pk, 0),
        );
        let (device_dz_prefix_ext, _, _) = get_resource_extension_pda(
            &client.get_program_id(),
            ResourceType::DzPrefixBlock(device_pk, 0),
        );

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

        // Mock GlobalConfig fetch (to check if dz_ip is in multicast_publisher_block)
        let (globalconfig_pubkey, bump_seed) = get_globalconfig_pda(&client.get_program_id());
        let globalconfig = GlobalConfig {
            account_type: AccountType::GlobalConfig,
            owner: Pubkey::default(),
            bump_seed,
            local_asn: 0,
            remote_asn: 0,
            device_tunnel_block: "1.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "2.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/24".parse().unwrap(),
            multicast_publisher_block: "147.51.126.0/23".parse().unwrap(), // dz_ip 10.0.0.1 NOT in this range
            next_bgp_community: 0,
        };
        client
            .expect_get()
            .with(predicate::eq(globalconfig_pubkey))
            .returning(move |_| Ok(AccountData::GlobalConfig(globalconfig.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CloseAccountUser(
                    UserCloseAccountArgs {
                        dz_prefix_count: 1,
                        multicast_publisher_count: 0,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(owner, false),
                    AccountMeta::new(device_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(global_resource_ext, false),
                    AccountMeta::new(device_tunnel_ids_ext, false),
                    AccountMeta::new(device_dz_prefix_ext, false),
                    AccountMeta::new(tenant_pk, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CloseAccountUserCommand {
            pubkey: user_pubkey,
            use_onchain_deallocation: true,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
