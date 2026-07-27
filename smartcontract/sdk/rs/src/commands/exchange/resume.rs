use crate::DoubleZeroClient;
use doublezero_serviceability::processors::exchange::resume::ExchangeResumeArgs;
use doublezero_serviceability_instruction::exchange::resume_exchange;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct ResumeExchangeCommand {
    pub pubkey: Pubkey,
}

impl ResumeExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(resume_exchange(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            ExchangeResumeArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::exchange::resume::ResumeExchangeCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_exchange_pda, processors::exchange::resume::ExchangeResumeArgs,
    };
    use doublezero_serviceability_instruction::exchange::resume_exchange;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_exchange_resume_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_exchange_pda(&program_id, 1);

        let expected = resume_exchange(&program_id, &payer, &pda_pubkey, ExchangeResumeArgs {});
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = ResumeExchangeCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
