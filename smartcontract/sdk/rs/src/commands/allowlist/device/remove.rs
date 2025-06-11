use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::allowlist::device::remove::RemoveDeviceAllowlistArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct RemoveDeviceAllowlistCommand {
    pub pubkey: Pubkey,
}

impl RemoveDeviceAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_transaction(
            DoubleZeroInstruction::RemoveDeviceAllowlist(RemoveDeviceAllowlistArgs {
                pubkey: self.pubkey,
            }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }
}
