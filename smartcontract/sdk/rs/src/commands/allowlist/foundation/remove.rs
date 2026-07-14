use doublezero_serviceability::processors::allowlist::foundation::remove::RemoveFoundationAllowlistArgs;
use doublezero_serviceability_instruction::allowlist::remove_foundation_allowlist;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct RemoveFoundationAllowlistCommand {
    pub pubkey: Pubkey,
}

impl RemoveFoundationAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(remove_foundation_allowlist(
            &client.get_program_id(),
            &client.get_payer(),
            RemoveFoundationAllowlistArgs {
                pubkey: self.pubkey,
            },
        ))
    }
}
