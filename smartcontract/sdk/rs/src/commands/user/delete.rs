use std::{net::Ipv4Addr, time::Duration};

use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, globalstate::get::GetGlobalStateCommand,
        multicastgroup::subscribe::SubscribeMulticastGroupCommand, user::get::GetUserCommand,
    },
    DoubleZeroClient, UserStatus,
};
use backon::{BlockingRetryable, ExponentialBuilder};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::user::delete::UserDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteUserCommand {
    pub pubkey: Pubkey,
}

impl DeleteUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let user = client
            .get(self.pubkey)
            .map_err(|_| eyre::eyre!("User not found ({})", self.pubkey))?
            .get_user()
            .map_err(|e| eyre::eyre!(e))?;

        for mgroup_pk in user.publishers.iter().chain(user.subscribers.iter()) {
            SubscribeMulticastGroupCommand {
                group_pk: *mgroup_pk,
                user_pk: self.pubkey,
                client_ip: user.client_ip,
                publisher: false,
                subscriber: false,
            }
            .execute(client)?;
        }

        if !user.publishers.is_empty() || !user.subscribers.is_empty() {
            // timings are set to handle expected worst case activator reactions
            let builder = ExponentialBuilder::new()
                .with_max_times(8) // 1+2+4+8+16+32+32+32 = 127 seconds max
                .with_min_delay(Duration::from_secs(1))
                .with_max_delay(Duration::from_secs(32));

            // need to wait until activator is done and changes status from Updating
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

        client.execute_transaction(
            DoubleZeroInstruction::DeleteUser(UserDeleteArgs {}),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(accesspass_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::delete::DeleteUserCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_accesspass_pda, get_globalstate_pda, get_multicastgroup_pda},
        processors::{
            multicastgroup::subscribe::MulticastGroupSubscribeArgs, user::delete::UserDeleteArgs,
        },
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_delete_multicast_user_retries_until_status_activated() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let user_pubkey = Pubkey::new_unique();
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&client.get_program_id(), 1);
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        // User with one subscriber - triggers the retry logic
        let user_activated_with_sub = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk: Pubkey::default(),
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![mgroup_pubkey],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        // User with Updating status (returned by first retry call)
        let user_updating = User {
            status: UserStatus::Updating,
            subscribers: vec![], // After unsubscribe, empty
            ..user_activated_with_sub.clone()
        };

        // User with Activated status (returned by second retry call)
        let user_activated_final = User {
            status: UserStatus::Activated,
            subscribers: vec![],
            ..user_activated_with_sub.clone()
        };

        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            code: "test".to_string(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            tenant_pk: Pubkey::default(),
            multicast_ip: "223.0.0.1".parse().unwrap(),
            publisher_count: 0,
            subscriber_count: 1,
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
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: 0,
        };

        let mut seq = Sequence::new();

        // Call 1: Initial user fetch in DeleteUserCommand - Activated with subscriber
        let user_clone1 = user_activated_with_sub.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone1.clone())));

        // Call 2: MulticastGroup fetch in SubscribeMulticastGroupCommand
        let mgroup_clone = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(mgroup_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup_clone.clone())));

        // Call 3: User fetch inside SubscribeMulticastGroupCommand - needs Activated
        let user_clone2 = user_activated_with_sub.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone2.clone())));

        // Call 4: AccessPass fetch in SubscribeMulticastGroupCommand
        let accesspass_clone1 = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone1.clone())));

        // Execute transaction for SubscribeMulticastGroupCommand (unsubscribe)
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SubscribeMulticastGroup(
                    MulticastGroupSubscribeArgs {
                        publisher: false,
                        subscriber: false,
                        client_ip,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(mgroup_pubkey, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(user_pubkey, false),
                ]),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        // Call 5: First retry GetUserCommand - returns Updating (triggers retry)
        let user_updating_clone = user_updating.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_updating_clone.clone())));

        // Call 6: Second retry GetUserCommand - returns Activated (success)
        let user_final_clone = user_activated_final.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_final_clone.clone())));

        // Call 7: AccessPass fetch for DeleteUserCommand
        let accesspass_clone2 = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone2.clone())));

        // Execute transaction for DeleteUser
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteUser(UserDeleteArgs {})),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteUserCommand {
            pubkey: user_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
