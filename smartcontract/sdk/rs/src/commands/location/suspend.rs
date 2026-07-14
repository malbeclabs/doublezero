use crate::DoubleZeroClient;
use doublezero_serviceability::processors::location::suspend::LocationSuspendArgs;
use doublezero_serviceability_instruction::location::suspend_location;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SuspendLocationCommand {
    pub pubkey: Pubkey,
}

impl SuspendLocationCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(suspend_location(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            LocationSuspendArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::location::suspend::SuspendLocationCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_location_pda, processors::location::suspend::LocationSuspendArgs,
    };
    use doublezero_serviceability_instruction::location::suspend_location;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_location_suspend_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_location_pda(&program_id, 1);

        // The command must build exactly the builder's instruction and hand it to
        // send_transaction (which no longer touches account layout).
        let expected = suspend_location(&program_id, &payer, &pda_pubkey, LocationSuspendArgs {});
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SuspendLocationCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
