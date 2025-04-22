use double_zero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::globalstate::foundation_allowlist::remove::RemoveFoundationAllowlistGlobalConfigArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

pub struct RemoveFoundationAllowlistCommand {
    pub pubkey: Pubkey,
}

impl RemoveFoundationAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_transaction(
            DoubleZeroInstruction::RemoveFoundationAllowlistGlobalConfig(
                RemoveFoundationAllowlistGlobalConfigArgs { pubkey: self.pubkey },
            ),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
            
    }
}
