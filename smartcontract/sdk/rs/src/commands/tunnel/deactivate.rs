use double_zero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_tunnel_pda,
    processors::tunnel::deactivate::TunnelDeactivateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

pub struct DeactivateTunnelCommand {
    pub index: u128,
    pub owner: Pubkey,
}

impl DeactivateTunnelCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) =
            get_tunnel_pda(&client.get_program_id(), self.index);
        client
            .execute_transaction(
                DoubleZeroInstruction::DeactivateTunnel(TunnelDeactivateArgs { index: self.index }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(self.owner, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            
    }
}
