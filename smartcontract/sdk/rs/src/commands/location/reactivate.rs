use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_location_pda,
    processors::location::reactivate::LocationReactivateArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

pub struct ReactivateLocationCommand {
    pub index: u128,
}

impl ReactivateLocationCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) = get_location_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::ReactivateLocation(LocationReactivateArgs { index: self.index }),
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
        commands::location::reactivate::ReactivateLocationCommand,
        tests::tests::create_test_client, DoubleZeroClient,
    };
    use doublezero_sla_program::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda},
        processors::location::reactivate::LocationReactivateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature, system_program};

    #[test]
    fn test_commands_location_reactivate_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_location_pda(&client.get_program_id(), 1);
        let payer = client.get_payer();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ReactivateLocation(
                    LocationReactivateArgs { index: 1 },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(payer, true),
                    AccountMeta::new(system_program::id(), false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ReactivateLocationCommand { index: 1 }.execute(&client);

        assert!(res.is_ok());
    }
}
