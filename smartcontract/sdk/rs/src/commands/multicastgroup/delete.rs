use crate::{
    commands::{
        globalstate::get::GetGlobalStateCommand,
        multicastgroup::subscribe::SubscribeMulticastGroupCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_multicastgroup_pda,
    processors::multicastgroup::delete::MulticastGroupDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteMulticastGroupCommand {
    pub index: u128,
}

impl DeleteMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (mgroup_pubkey, bump_seed) =
            get_multicastgroup_pda(&client.get_program_id(), self.index);

        let mgroup = client
            .get(mgroup_pubkey)
            .map_err(|_| eyre::eyre!("MulticastGroup not found ({})", mgroup_pubkey))?
            .get_multicastgroup();

        for user_pk in mgroup.publishers.iter().chain(mgroup.subscribers.iter()) {
            SubscribeMulticastGroupCommand {
                group_pk: mgroup_pubkey,
                user_pk: *user_pk,
                publisher: false,
                subscriber: false,
            }
            .execute(client)?;
        }

        client.execute_transaction(
            DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {
                index: self.index,
                bump_seed,
            }),
            vec![
                AccountMeta::new(mgroup_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::delete::DeleteMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_multicastgroup_pda},
        processors::multicastgroup::delete::MulticastGroupDeleteArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_delete_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_multicastgroup_pda(&client.get_program_id(), 1);

        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 2,
            bump_seed,
            tenant_pk: Pubkey::default(),
            code: "mg01".to_string(),
            multicast_ip: [0, 0, 0, 0],
            max_bandwidth: 0,
            status: MulticastGroupStatus::Activated,
            pub_allowlist: vec![client.get_payer()],
            sub_allowlist: vec![client.get_payer()],
            publishers: vec![],
            subscribers: vec![],
            owner: Pubkey::default(),
        };

        let mgroup_cloned = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup_cloned.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteMulticastGroup(
                    MulticastGroupDeleteArgs {
                        index: 1,
                        bump_seed,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteMulticastGroupCommand { index: 1 }.execute(&client);

        assert!(res.is_ok());
    }
}
