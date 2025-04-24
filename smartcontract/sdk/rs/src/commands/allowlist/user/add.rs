use double_zero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::allowlist::user::remove::RemoveUserAllowlistGlobalConfigArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

pub struct AddUserAllowlistCommand {
    pub pubkey: Pubkey,
}

impl AddUserAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client
            .execute_transaction(
                DoubleZeroInstruction::RemoveUserAllowlistGlobalConfig(
                    RemoveUserAllowlistGlobalConfigArgs {
                        pubkey: self.pubkey,
                    },
                ),
                vec![AccountMeta::new(pda_pubkey, false)],
            )
            
    }
}
