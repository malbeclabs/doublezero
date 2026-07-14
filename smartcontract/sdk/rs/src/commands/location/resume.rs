use crate::DoubleZeroClient;
use doublezero_serviceability::processors::location::resume::LocationResumeArgs;
use doublezero_serviceability_instruction::location::resume_location;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct ResumeLocationCommand {
    pub pubkey: Pubkey,
}

impl ResumeLocationCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(resume_location(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            LocationResumeArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::location::resume::ResumeLocationCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_location_pda, processors::location::resume::LocationResumeArgs,
    };
    use doublezero_serviceability_instruction::location::resume_location;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_location_resume_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_location_pda(&program_id, 1);

        let expected = resume_location(&program_id, &payer, &pda_pubkey, LocationResumeArgs {});
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = ResumeLocationCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
