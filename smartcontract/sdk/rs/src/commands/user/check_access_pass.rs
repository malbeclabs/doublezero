use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, user::get::GetUserCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_accesspass_pda,
    processors::user::check_access_pass::CheckUserAccessPassArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CheckUserAccessPassCommand {
    pub user_pubkey: Pubkey,
}

impl CheckUserAccessPassCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, user) = GetUserCommand {
            pubkey: self.user_pubkey,
        }
        .execute(client)?;

        let (accesspass_pk, _) =
            get_accesspass_pda(&client.get_program_id(), &user.client_ip, &user.owner);

        client.execute_transaction(
            DoubleZeroInstruction::CheckUserAccessPass(CheckUserAccessPassArgs {}),
            vec![
                AccountMeta::new(self.user_pubkey, false),
                AccountMeta::new(accesspass_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
