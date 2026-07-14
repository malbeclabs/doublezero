use doublezero_serviceability::processors::allowlist::qa::add::AddQaAllowlistArgs;
use doublezero_serviceability_instruction::allowlist::add_qa_allowlist;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct AddQaAllowlistCommand {
    pub pubkey: Pubkey,
}

impl AddQaAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(add_qa_allowlist(
            &client.get_program_id(),
            &client.get_payer(),
            AddQaAllowlistArgs {
                pubkey: self.pubkey,
            },
        ))
    }
}
