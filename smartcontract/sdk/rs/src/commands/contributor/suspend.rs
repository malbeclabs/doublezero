use crate::DoubleZeroClient;
use doublezero_serviceability::processors::contributor::suspend::ContributorSuspendArgs;
use doublezero_serviceability_instruction::contributor::suspend_contributor;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SuspendContributorCommand {
    pub pubkey: Pubkey,
}

impl SuspendContributorCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(suspend_contributor(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            ContributorSuspendArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::contributor::suspend::SuspendContributorCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_contributor_pda, processors::contributor::suspend::ContributorSuspendArgs,
    };
    use doublezero_serviceability_instruction::contributor::suspend_contributor;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_contributor_suspend_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_contributor_pda(&program_id, 1);

        let expected =
            suspend_contributor(&program_id, &payer, &pda_pubkey, ContributorSuspendArgs {});
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SuspendContributorCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
