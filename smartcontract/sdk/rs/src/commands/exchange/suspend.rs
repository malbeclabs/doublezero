use crate::DoubleZeroClient;
use doublezero_serviceability::processors::exchange::suspend::ExchangeSuspendArgs;
use doublezero_serviceability_instruction::exchange::suspend_exchange;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SuspendExchangeCommand {
    pub pubkey: Pubkey,
}

impl SuspendExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(suspend_exchange(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            ExchangeSuspendArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::exchange::suspend::SuspendExchangeCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_exchange_pda, processors::exchange::suspend::ExchangeSuspendArgs,
    };
    use doublezero_serviceability_instruction::exchange::suspend_exchange;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_exchange_suspend_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_exchange_pda(&program_id, 1);

        let expected = suspend_exchange(&program_id, &payer, &pda_pubkey, ExchangeSuspendArgs {});
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SuspendExchangeCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
