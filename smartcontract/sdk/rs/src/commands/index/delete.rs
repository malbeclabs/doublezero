use crate::DoubleZeroClient;
use doublezero_serviceability::processors::index::delete::IndexDeleteArgs;
use doublezero_serviceability_instruction::index::delete_index;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteIndexCommand {
    pub index_pubkey: Pubkey,
}

impl DeleteIndexCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(delete_index(
            &client.get_program_id(),
            &client.get_payer(),
            &self.index_pubkey,
            IndexDeleteArgs {},
        ))
    }
}
