use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_location_pda,
    processors::location::delete::LocationDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

pub struct DeleteLocationCommand {
    pub index: u128,
}

impl DeleteLocationCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) = get_location_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::DeleteLocation(LocationDeleteArgs { index: self.index }),
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
        commands::location::delete::DeleteLocationCommand, tests::tests::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_sla_program::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda},
        processors::location::delete::LocationDeleteArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature, system_program};

    #[test]
    fn test_commands_location_delete_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_location_pda(&client.get_program_id(), 1);
        let payer = client.get_payer();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteLocation(LocationDeleteArgs {
                    index: 1,
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(payer, true),
                    AccountMeta::new(system_program::id(), false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteLocationCommand { index: 1 }.execute(&client);

        assert!(res.is_ok());
    }
}
