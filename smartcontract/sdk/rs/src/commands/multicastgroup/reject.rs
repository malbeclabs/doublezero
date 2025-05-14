use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_multicastgroup_pda,
    processors::multicastgroup::reject::MulticastGroupRejectArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct RejectMulticastGroupCommand {
    pub index: u128,
    pub reason: String,
}

impl RejectMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) = get_multicastgroup_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::RejectMulticastGroup(MulticastGroupRejectArgs {
                index: self.index,
                bump_seed,
                reason: self.reason.clone(),
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
        commands::multicastgroup::reject::RejectMulticastGroupCommand,
        tests::tests::create_test_client, DoubleZeroClient,
    };
    use doublezero_sla_program::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda},
        processors::multicastgroup::reject::MulticastGroupRejectArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_location_reject_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_location_pda(&client.get_program_id(), 1);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::RejectMulticastGroup(
                    MulticastGroupRejectArgs {
                        index: 1,
                        bump_seed,
                        reason: "Test reason".to_string(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = RejectMulticastGroupCommand {
            index: 1,
            reason: "Test reason".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
