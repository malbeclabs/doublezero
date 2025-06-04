use doublezero_sla_program::{
    instructions::DoubleZeroInstruction,
    processors::multicastgroup::subscribe::MulticastGroupSubscribeArgs,
    state::{multicastgroup::MulticastGroupStatus, user::UserStatus},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, user::get::GetUserCommand},
    DoubleZeroClient,
};

use super::get::GetMulticastGroupCommand;

#[derive(Debug, PartialEq, Clone)]
pub struct SubscribeMulticastGroupCommand {
    pub group_pk: Pubkey,
    pub user_pk: Pubkey,
    pub publisher: bool,
    pub subscriber: bool,
}

impl SubscribeMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.group_pk.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("MulticastGroup not found"))?;

        if mgroup.status != MulticastGroupStatus::Activated {
            return Err(eyre::eyre!("MulticastGroup not active"));
        }
        if self.publisher && !mgroup.pub_allowlist.contains(&client.get_payer()) {
            return Err(eyre::eyre!("Publisher not allowed"));
        }
        if self.subscriber && !mgroup.sub_allowlist.contains(&client.get_payer()) {
            return Err(eyre::eyre!("Subscriber not allowed"));
        }

        let (_, user) = GetUserCommand {
            pubkey: self.user_pk,
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("User not found"))?;

        if user.status != UserStatus::Activated {
            return Err(eyre::eyre!("User not active"));
        }

        client.execute_transaction(
            DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
                publisher: self.publisher,
                subscriber: self.subscriber,
            }),
            vec![
                AccountMeta::new(self.group_pk, false),
                AccountMeta::new(self.user_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::subscribe::SubscribeMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_sla_program::state::accountdata::AccountData;
    use doublezero_sla_program::state::accounttype::AccountType;
    use doublezero_sla_program::state::multicastgroup::MulticastGroup;
    use doublezero_sla_program::state::multicastgroup::MulticastGroupStatus;
    use doublezero_sla_program::state::user::User;
    use doublezero_sla_program::state::user::UserCYOA;
    use doublezero_sla_program::state::user::UserStatus;
    use doublezero_sla_program::state::user::UserType;
    use doublezero_sla_program::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda},
        processors::multicastgroup::subscribe::MulticastGroupSubscribeArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_subscribe_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _bump_seed) = get_location_pda(&client.get_program_id(), 1);
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            code: "test".to_string(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            pub_allowlist: vec![client.get_payer()],
            sub_allowlist: vec![client.get_payer()],
            tenant_pk: Pubkey::default(),
            multicast_ip: [223, 0, 0, 1],
            publishers: vec![],
            subscribers: vec![],
        };

        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup.clone())));

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk: pda_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [0, 0, 0, 0],
            dz_ip: [0, 0, 0, 0],
            tunnel_id: 0,
            tunnel_net: ([0, 0, 0, 0], 0),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
        };
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SubscribeMulticastGroup(
                    MulticastGroupSubscribeArgs {
                        publisher: true,
                        subscriber: false,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SubscribeMulticastGroupCommand {
            group_pk: pda_pubkey,
            user_pk: user_pubkey,
            publisher: true,
            subscriber: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
