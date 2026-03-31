use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::index::delete::IndexDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteIndexCommand {
    pub index_pubkey: Pubkey,
}

impl DeleteIndexCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let accounts = vec![
            AccountMeta::new(self.index_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ];

        client.execute_transaction(
            DoubleZeroInstruction::DeleteIndex(IndexDeleteArgs {}),
            accounts,
        )
    }
}
