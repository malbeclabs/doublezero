use doublezero_sla_program::{instructions::DoubleZeroInstruction, pda::get_globalstate_pda};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::DoubleZeroClient;

pub struct InitGlobalStateCommand {}

impl InitGlobalStateCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_transaction(
            DoubleZeroInstruction::InitGlobalState(),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }
}
