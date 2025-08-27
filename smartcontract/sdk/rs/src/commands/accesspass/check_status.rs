use std::net::Ipv4Addr;

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_accesspass_pda,
    processors::accesspass::check_status::CheckStatusAccessPassArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CheckStatusAccessPassCommand {
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
}

impl CheckStatusAccessPassCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &self.client_ip, &self.user_payer);

        client.execute_transaction(
            DoubleZeroInstruction::CheckStatusAccessPass(CheckStatusAccessPassArgs {}),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new_readonly(globalstate_pubkey, false),
                AccountMeta::new(self.user_payer, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::check_status::CheckStatusAccessPassCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_accesspass_pda, get_globalstate_pda},
        processors::accesspass::check_status::CheckStatusAccessPassArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_expire_command() {
        let mut client = create_test_client();

        let client_ip = [10, 0, 0, 1].into();
        let payer = Pubkey::new_unique();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_accesspass_pda(&client.get_program_id(), &client_ip, &payer);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CheckStatusAccessPass(
                    CheckStatusAccessPassArgs {},
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(payer, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CheckStatusAccessPassCommand {
            client_ip,
            user_payer: payer,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
