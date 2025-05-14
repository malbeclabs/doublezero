use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_multicastgroup_pda,
    processors::multicastgroup::subscribe::MulticastGroupSubscribeArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct SubscribeMulticastGroupCommand {
    pub index: u128,
    pub publishers: Vec<Pubkey>,
    pub subscribers: Vec<Pubkey>,
}

impl SubscribeMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) = get_multicastgroup_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
                index: self.index,
                bump_seed,
                publishers: self.publishers.clone(),
                subscribers: self.subscribers.clone(),
            }),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::subscribe::SubscribeMulticastGroupCommand,
        tests::tests::create_test_client, DoubleZeroClient,
    };
    use doublezero_sla_program::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda},
        processors::multicastgroup::subscribe::MulticastGroupSubscribeArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_location_subscribe_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_location_pda(&client.get_program_id(), 1);

        let pubkey1 = Pubkey::new_unique();
        let pubkey2 = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SubscribeMulticastGroup(
                    MulticastGroupSubscribeArgs {
                        index: 1,
                        bump_seed,
                        publishers: vec![pubkey1, pubkey2],
                        subscribers: vec![],
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SubscribeMulticastGroupCommand {
            index: 1,
            publishers: vec![pubkey1, pubkey2],
            subscribers: vec![],
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
