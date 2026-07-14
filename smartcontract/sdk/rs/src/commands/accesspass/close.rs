use crate::DoubleZeroClient;
use doublezero_serviceability::processors::accesspass::close::CloseAccessPassArgs;
use doublezero_serviceability_instruction::accesspass::close_access_pass;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CloseAccessPassCommand {
    pub pubkey: Pubkey,
}

impl CloseAccessPassCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(close_access_pass(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            CloseAccessPassArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::close::CloseAccessPassCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_accesspass_pda, processors::accesspass::close::CloseAccessPassArgs,
    };
    use doublezero_serviceability_instruction::accesspass::close_access_pass;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_close_accesspass_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let client_ip = [10, 0, 0, 1].into();
        let user_payer = Pubkey::new_unique();

        let (pda_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

        let expected = close_access_pass(&program_id, &payer, &pda_pubkey, CloseAccessPassArgs {});
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = CloseAccessPassCommand { pubkey: pda_pubkey }.execute(&client);
        assert!(res.is_ok());
    }
}
