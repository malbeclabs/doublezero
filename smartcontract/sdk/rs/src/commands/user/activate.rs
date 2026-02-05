use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, device::get::GetDeviceCommand,
        globalstate::get::GetGlobalStateCommand, user::get::GetUserCommand,
    },
    DoubleZeroClient,
};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    error::DoubleZeroError, instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::user::activate::UserActivateArgs, resource::ResourceType, state::user::UserStatus,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct ActivateUserCommand {
    pub user_pubkey: Pubkey,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    pub dz_ip: Ipv4Addr,
    /// When true, SDK computes ResourceExtension PDAs and includes them for on-chain allocation.
    /// When false, uses legacy behavior with caller-provided tunnel_id, tunnel_net, and dz_ip.
    pub use_onchain_allocation: bool,
}

impl ActivateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, user) = GetUserCommand {
            pubkey: self.user_pubkey,
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("User not found"))?;

        // Pre-flight check: only attempt activation if user is in a valid status
        if user.status != UserStatus::Pending && user.status != UserStatus::Updating {
            return Err(DoubleZeroError::InvalidStatus.into());
        }

        let (accesspass_pk, _) = GetAccessPassCommand {
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: user.owner,
        }
        .execute(client)
        .or_else(|_| {
            GetAccessPassCommand {
                client_ip: user.client_ip,
                user_payer: user.owner,
            }
            .execute(client)
        })
        .map_err(|_| eyre::eyre!("You have no Access Pass"))?;

        // Build accounts list with optional ResourceExtension accounts before payer
        // (payer and system_program are appended by execute_transaction)
        let mut accounts = vec![
            AccountMeta::new(self.user_pubkey, false),
            AccountMeta::new(accesspass_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        let dz_prefix_count: u8 = if self.use_onchain_allocation {
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

            // Device TunnelIds (scoped to user's device)
            let (device_tunnel_ids_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::TunnelIds(user.device_pk, 0),
            );

            accounts.push(AccountMeta::new(global_resource_ext, false));
            accounts.push(AccountMeta::new(device_tunnel_ids_ext, false));

            // Add all N DzPrefixBlock accounts (devices can have multiple dz_prefixes)
            for idx in 0..count {
                let (device_dz_prefix_ext, _, _) = get_resource_extension_pda(
                    &client.get_program_id(),
                    ResourceType::DzPrefixBlock(user.device_pk, idx),
                );
                accounts.push(AccountMeta::new(device_dz_prefix_ext, false));
            }

            count as u8
        } else {
            0
        };

        client.execute_transaction(
            DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
                dz_ip: self.dz_ip,
                dz_prefix_count,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::activate::ActivateUserCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        error::DoubleZeroError,
        instructions::DoubleZeroInstruction,
        pda::{get_accesspass_pda, get_globalstate_pda, get_resource_extension_pda},
        processors::user::activate::UserActivateArgs,
        resource::ResourceType,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
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
    fn test_commands_user_activate_without_resource_extension() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);
        let tunnel_id: u16 = 100;
        let tunnel_net: NetworkV4 = "10.0.0.0/24".parse().unwrap();
        let dz_ip = Ipv4Addr::new(10, 0, 0, 1);

        let user = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: Ipv4Addr::UNSPECIFIED,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Pending,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(
            &client.get_program_id(),
            &Ipv4Addr::UNSPECIFIED,
            &client.get_payer(),
        );
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: client.get_payer(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: client.get_payer(),
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
        };

        // Mock User fetch
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        // Mock AccessPass fetch (UNSPECIFIED IP path)
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                    tunnel_id,
                    tunnel_net,
                    dz_ip,
                    dz_prefix_count: 0,
                })),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ActivateUserCommand {
            user_pubkey,
            tunnel_id,
            tunnel_net,
            dz_ip,
            use_onchain_allocation: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_user_activate_with_onchain_allocation() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let user = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: Ipv4Addr::UNSPECIFIED,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Pending,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(
            &client.get_program_id(),
            &Ipv4Addr::UNSPECIFIED,
            &client.get_payer(),
        );
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: client.get_payer(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: client.get_payer(),
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
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

        // Mock AccessPass fetch (UNSPECIFIED IP path)
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

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

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                    tunnel_id: 0,
                    tunnel_net: NetworkV4::default(),
                    dz_ip: Ipv4Addr::UNSPECIFIED,
                    dz_prefix_count: 1, // 1 dz_prefix from device.dz_prefixes
                })),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(global_resource_ext, false),
                    AccountMeta::new(device_tunnel_ids_ext, false),
                    AccountMeta::new(device_dz_prefix_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ActivateUserCommand {
            user_pubkey,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            dz_ip: Ipv4Addr::UNSPECIFIED,
            use_onchain_allocation: true,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_user_activate_preflight_rejects_already_activated() {
        let mut client = create_test_client();

        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();

        let user = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: Ipv4Addr::new(192, 168, 1, 10),
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

        // Mock User fetch — returns user in Activated status
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        let res = ActivateUserCommand {
            user_pubkey,
            tunnel_id: 100,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            use_onchain_allocation: false,
        }
        .execute(&client);

        assert!(res.is_err());
        let err = res.unwrap_err();
        let dz_err = err.downcast_ref::<DoubleZeroError>().unwrap();
        assert_eq!(*dz_err, DoubleZeroError::InvalidStatus);
    }

    #[test]
    fn test_commands_user_activate_preflight_rejects_out_of_credits() {
        let mut client = create_test_client();

        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();

        let user = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: Ipv4Addr::new(192, 168, 1, 10),
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            status: UserStatus::OutOfCredits,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

        // Mock User fetch — returns user in OutOfCredits status
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        let res = ActivateUserCommand {
            user_pubkey,
            tunnel_id: 100,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            use_onchain_allocation: false,
        }
        .execute(&client);

        assert!(res.is_err());
        let err = res.unwrap_err();
        let dz_err = err.downcast_ref::<DoubleZeroError>().unwrap();
        assert_eq!(*dz_err, DoubleZeroError::InvalidStatus);
    }
}
