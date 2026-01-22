use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::allowlist::qa::remove::RemoveQaAllowlistArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct RemoveQaAllowlistCommand {
    pub pubkey: Pubkey,
}

impl RemoveQaAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_transaction(
            DoubleZeroInstruction::RemoveQaAllowlist(RemoveQaAllowlistArgs {
                pubkey: self.pubkey,
            }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }
}
