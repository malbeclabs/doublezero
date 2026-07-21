use doublezero_serviceability::processors::allowlist::qa::remove::RemoveQaAllowlistArgs;
use doublezero_serviceability_instruction::allowlist::remove_qa_allowlist;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct RemoveQaAllowlistCommand {
    pub pubkey: Pubkey,
}

impl RemoveQaAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(remove_qa_allowlist(
            &client.get_program_id(),
            &client.get_payer(),
            RemoveQaAllowlistArgs {
                pubkey: self.pubkey,
            },
        ))
    }
}
