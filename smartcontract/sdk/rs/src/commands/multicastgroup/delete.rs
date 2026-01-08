use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::multicastgroup::delete::MulticastGroupDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteMulticastGroupCommand {
    pub pubkey: Pubkey,
}

impl DeleteMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let mgroup_pubkey = self.pubkey;

        // TODO: Check for existing AccessPass referencing this multicast group pubkey in either publishers or subscribers lists before deletion

        client.execute_transaction(
            DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {}),
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
    use std::net::Ipv4Addr;

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
            multicast_ip: Ipv4Addr::UNSPECIFIED,
            max_bandwidth: 0,
            status: MulticastGroupStatus::Activated,
            owner: Pubkey::default(),
            publisher_count: 1,
            subscriber_count: 0,
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
                    MulticastGroupDeleteArgs {},
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteMulticastGroupCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
