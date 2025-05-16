use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::multicastgroup::allowlist::subscriber::remove::RemoveMulticastGroupSubAllowlistArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct RemoveMulticastGroupSubAllowlistCommand {
    pub pubkey: Pubkey,
}

impl RemoveMulticastGroupSubAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_transaction(
            DoubleZeroInstruction::RemoveMulticastGroupSubAllowlist(
                RemoveMulticastGroupSubAllowlistArgs {
                    pubkey: self.pubkey,
                },
            ),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }
}
