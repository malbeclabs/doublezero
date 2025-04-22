use double_zero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::globalstate::device_allowlist::remove::RemoveDeviceAllowlistGlobalConfigArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

pub struct AddDeviceAllowlistCommand {
    pub pubkey: Pubkey,
}

impl AddDeviceAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client
            .execute_transaction(
                DoubleZeroInstruction::RemoveDeviceAllowlistGlobalConfig(
                    RemoveDeviceAllowlistGlobalConfigArgs {
                        pubkey: self.pubkey,
                    },
                ),
                vec![AccountMeta::new(pda_pubkey, false)],
            )
            
    }
}
