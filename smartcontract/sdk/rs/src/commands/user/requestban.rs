use std::{collections::HashSet, time::Duration};

use crate::{
    commands::{
        device::get::GetDeviceCommand,
        globalstate::get::GetGlobalStateCommand,
        multicastgroup::{
            list::ListMulticastGroupCommand, subscribe::SubscribeMulticastGroupCommand,
        },
        user::get::GetUserCommand,
    },
    DoubleZeroClient, UserStatus,
};
use backon::{BlockingRetryable, ExponentialBuilder};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_resource_extension_pda,
    processors::user::requestban::UserRequestBanArgs,
    resource::ResourceType,
    state::feature_flags::{is_feature_enabled, FeatureFlag},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct RequestBanUserCommand {
    pub pubkey: Pubkey,
}

impl RequestBanUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let use_onchain_deallocation =
            is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation);

        let user = client
            .get(self.pubkey)
            .map_err(|_| eyre::eyre!("User not found ({})", self.pubkey))?
            .get_user()
            .map_err(|e| eyre::eyre!(e))?;

        if use_onchain_deallocation {
            // Handle multicast unsubscription before banning
            let unique_mgroup_pks: Vec<Pubkey> = user
                .publishers
                .iter()
                .chain(user.subscribers.iter())
                .copied()
                .collect::<HashSet<_>>()
                .into_iter()
                .collect();
            let multicastgroups = ListMulticastGroupCommand {}.execute(client)?;
            for mgroup_pk in &unique_mgroup_pks {
                if multicastgroups.contains_key(mgroup_pk) {
                    SubscribeMulticastGroupCommand {
                        group_pk: *mgroup_pk,
                        user_pk: self.pubkey,
                        client_ip: user.client_ip,
                        publisher: false,
                        subscriber: false,
                    }
                    .execute(client)?;
                }
            }

            if !user.publishers.is_empty() || !user.subscribers.is_empty() {
                let builder = ExponentialBuilder::new()
                    .with_max_times(8)
                    .with_min_delay(Duration::from_secs(1))
                    .with_max_delay(Duration::from_secs(32));

                let get_user = || match (GetUserCommand {
                    pubkey: self.pubkey,
                })
                .execute(client)
                {
                    Ok((_, user)) => {
                        if user.status == UserStatus::Updating {
                            Err(())
                        } else {
                            Ok(user)
                        }
                    }
                    Err(_) => Err(()),
                };

                let _ = get_user
                    .retry(builder)
                    .call()
                    .map_err(|_| eyre::eyre!("Timeout waiting for user multicast unsubscribe"))?;
            }
        }

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        let (dz_prefix_count, multicast_publisher_count) = if use_onchain_deallocation {
            let (_, device) = GetDeviceCommand {
                pubkey_or_code: user.device_pk.to_string(),
            }
            .execute(client)
            .map_err(|_| eyre::eyre!("Device not found"))?;

            let count = device.dz_prefixes.len();

            // UserTunnelBlock (global)
            let (user_tunnel_block_ext, _, _) =
                get_resource_extension_pda(&client.get_program_id(), ResourceType::UserTunnelBlock);
            accounts.push(AccountMeta::new(user_tunnel_block_ext, false));

            // MulticastPublisherBlock (global) — always include for deallocation
            let (multicast_publisher_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::MulticastPublisherBlock,
            );
            accounts.push(AccountMeta::new(multicast_publisher_block_ext, false));

            // TunnelIds (per-device)
            let (device_tunnel_ids_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::TunnelIds(user.device_pk, 0),
            );
            accounts.push(AccountMeta::new(device_tunnel_ids_ext, false));

            // DzPrefixBlock accounts (per-device)
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

        client.execute_authorized_transaction(
            DoubleZeroInstruction::RequestBanUser(UserRequestBanArgs {
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
        commands::user::requestban::RequestBanUserCommand, tests::utils::create_test_client,
        DoubleZeroClient, MockDoubleZeroClient,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_resource_extension_pda},
        processors::user::requestban::UserRequestBanArgs,
        resource::ResourceType,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            feature_flags::FeatureFlag,
            globalstate::GlobalState,
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_request_ban_user_with_onchain_deallocation() {
        let mut client = MockDoubleZeroClient::new();

        let payer = Pubkey::new_unique();
        client.expect_get_payer().returning(move || payer);
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let (globalstate_pubkey, bump_seed) = get_globalstate_pda(&program_id);
        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed,
            account_index: 0,
            foundation_allowlist: vec![],
            _device_allowlist: vec![],
            _user_allowlist: vec![],
            activator_authority_pk: Pubkey::new_unique(),
            sentinel_authority_pk: Pubkey::new_unique(),
            contributor_airdrop_lamports: 1_000_000_000,
            user_airdrop_lamports: 40_000,
            health_oracle_pk: Pubkey::new_unique(),
            qa_allowlist: vec![],
            feature_flags: FeatureFlag::OnChainAllocation.to_mask(),
            reservation_authority_pk: Pubkey::default(),
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

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
            tunnel_id: 100,
            tunnel_net: "10.1.0.0/31".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        // Mock Device fetch (1 dz_prefix)
        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        // ListMulticastGroupCommand — no groups
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .returning(|_| Ok(std::collections::HashMap::new()));

        // Compute ResourceExtension PDAs
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
                predicate::eq(DoubleZeroInstruction::RequestBanUser(UserRequestBanArgs {
                    dz_prefix_count: 1,
                    multicast_publisher_count: 1,
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

        let res = RequestBanUserCommand {
            pubkey: user_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_request_ban_user_legacy() {
        let client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let user_pubkey = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let user = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk: Pubkey::default(),
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        let mut client = client;

        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::RequestBanUser(
                    UserRequestBanArgs::default(),
                )),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = RequestBanUserCommand {
            pubkey: user_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
