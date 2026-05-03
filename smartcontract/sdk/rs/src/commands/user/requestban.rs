use std::{collections::HashSet, time::Duration};

use crate::{
    commands::{
        device::get::GetDeviceCommand,
        globalstate::get::GetGlobalStateCommand,
        multicastgroup::{
            list::ListMulticastGroupCommand, subscribe::UpdateMulticastGroupRolesCommand,
        },
        user::get::GetUserCommand,
    },
    DoubleZeroClient, UserStatus,
};
use backon::{BlockingRetryable, ExponentialBuilder};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::user::requestban::UserRequestBanArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct RequestBanUserCommand {
    pub pubkey: Pubkey,
}

impl RequestBanUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let user = client
            .get(self.pubkey)
            .map_err(|_| eyre::eyre!("User not found ({})", self.pubkey))?
            .get_user()
            .map_err(|e| eyre::eyre!(e))?;

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
                UpdateMulticastGroupRolesCommand {
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

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: user.device_pk.to_string(),
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
            ResourceType::TunnelIds(user.device_pk, 0),
        );

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_ext, false),
            AccountMeta::new(multicast_publisher_block_ext, false),
            AccountMeta::new(device_tunnel_ids_ext, false),
        ];

        for idx in 0..dz_prefix_count {
            let (dz_prefix_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::DzPrefixBlock(user.device_pk, idx),
            );
            accounts.push(AccountMeta::new(dz_prefix_ext, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::RequestBanUser(UserRequestBanArgs {
                dz_prefix_count: dz_prefix_count as u8,
                multicast_publisher_count: 1,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::requestban::RequestBanUserCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_resource_extension_pda},
        processors::user::requestban::UserRequestBanArgs,
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
    fn test_request_ban_user() {
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
            tunnel_id: 100,
            tunnel_net: "10.1.0.0/31".parse().unwrap(),
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

        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .returning(|_| Ok(std::collections::HashMap::new()));

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
}
