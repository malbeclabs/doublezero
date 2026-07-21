use crate::{commands::user::get::GetUserCommand, DoubleZeroClient};
use doublezero_serviceability::{
    pda::get_accesspass_pda, processors::user::check_access_pass::CheckUserAccessPassArgs,
};
use doublezero_serviceability_instruction::user::check_user_access_pass;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CheckUserAccessPassCommand {
    pub user_pubkey: Pubkey,
}

impl CheckUserAccessPassCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (_, user) = GetUserCommand {
            pubkey: self.user_pubkey,
        }
        .execute(client)?;

        let program_id = client.get_program_id();
        let (accesspass_pk, _) = get_accesspass_pda(&program_id, &user.client_ip, &user.owner);

        client.send_transaction(check_user_access_pass(
            &program_id,
            &client.get_payer(),
            &self.user_pubkey,
            &accesspass_pk,
            CheckUserAccessPassArgs {},
        ))
    }
}
