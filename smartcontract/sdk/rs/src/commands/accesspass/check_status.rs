use crate::DoubleZeroClient;
use doublezero_serviceability::processors::accesspass::check_status::CheckStatusAccessPassArgs;
use doublezero_serviceability_instruction::accesspass::check_status_access_pass;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct CheckStatusAccessPassCommand {
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
}

impl CheckStatusAccessPassCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(check_status_access_pass(
            &client.get_program_id(),
            &client.get_payer(),
            self.client_ip,
            &self.user_payer,
            CheckStatusAccessPassArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::check_status::CheckStatusAccessPassCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::processors::accesspass::check_status::CheckStatusAccessPassArgs;
    use doublezero_serviceability_instruction::accesspass::check_status_access_pass;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_expire_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let client_ip = [10, 0, 0, 1].into();
        let user_payer = Pubkey::new_unique();

        let expected = check_status_access_pass(
            &program_id,
            &payer,
            client_ip,
            &user_payer,
            CheckStatusAccessPassArgs {},
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = CheckStatusAccessPassCommand {
            client_ip,
            user_payer,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
