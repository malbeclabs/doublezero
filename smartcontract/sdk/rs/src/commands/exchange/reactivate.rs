use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_exchange_pda,
    processors::exchange::reactivate::ExchangeReactivateArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct ReactivateExchangeCommand {
    pub index: u128,
}

impl ReactivateExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) = get_exchange_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::ReactivateExchange(ExchangeReactivateArgs {
                index: self.index,
                bump_seed,
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
        commands::exchange::reactivate::ReactivateExchangeCommand,
        tests::tests::create_test_client, DoubleZeroClient,
    };
    use doublezero_sla_program::{
        instructions::DoubleZeroInstruction,
        pda::{get_exchange_pda, get_globalstate_pda},
        processors::exchange::reactivate::ExchangeReactivateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature, system_program};

    #[test]
    fn test_commands_exchange_reactivate_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_exchange_pda(&client.get_program_id(), 1);
        let payer = client.get_payer();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ReactivateExchange(
                    ExchangeReactivateArgs {
                        index: 1,
                        bump_seed,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(payer, true),
                    AccountMeta::new(system_program::id(), false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ReactivateExchangeCommand { index: 1 }.execute(&client);

        assert!(res.is_ok());
    }
}
