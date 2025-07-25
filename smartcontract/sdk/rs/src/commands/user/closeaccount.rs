use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, user::get::GetUserCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::user::closeaccount::UserCloseAccountArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CloseAccountUserCommand {
    pub pubkey: Pubkey,
}

impl CloseAccountUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, user) = GetUserCommand {
            pubkey: self.pubkey,
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("User not found"))?;

        client.execute_transaction(
            DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {}),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(user.owner, false),
                AccountMeta::new(user.device_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
