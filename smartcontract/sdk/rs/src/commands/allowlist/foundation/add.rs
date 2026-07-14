use doublezero_serviceability::processors::allowlist::foundation::add::AddFoundationAllowlistArgs;
use doublezero_serviceability_instruction::allowlist::add_foundation_allowlist;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct AddFoundationAllowlistCommand {
    pub pubkey: Pubkey,
}

impl AddFoundationAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(add_foundation_allowlist(
            &client.get_program_id(),
            &client.get_payer(),
            AddFoundationAllowlistArgs {
                pubkey: self.pubkey,
            },
        ))
    }
}
