use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::allowlist::foundation::remove::RemoveFoundationAllowlistArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct RemoveFoundationAllowlistCommand {
    pub pubkey: Pubkey,
}

impl RemoveFoundationAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_transaction(
            DoubleZeroInstruction::RemoveFoundationAllowlist(RemoveFoundationAllowlistArgs {
                pubkey: self.pubkey,
            }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }
}
