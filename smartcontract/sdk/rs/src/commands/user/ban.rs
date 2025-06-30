use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::user::ban::UserBanArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct BanUserCommand {
    pub pubkey: Pubkey,
}

impl BanUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::BanUser(UserBanArgs {}),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
