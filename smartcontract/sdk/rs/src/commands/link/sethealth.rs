use crate::{DoubleZeroClient, GetGlobalStateCommand};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::link::sethealth::LinkSetHealthArgs,
    state::link::LinkHealth,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SetLinkHealthCommand {
    pub pubkey: Pubkey,
    pub health: LinkHealth,
}

impl SetLinkHealthCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::SetLinkHealth(LinkSetHealthArgs {
                health: self.health,
            }),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
