use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::link::activate::LinkActivateArgs,
    types::NetworkV4,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct ActivateLinkCommand {
    pub pubkey: Pubkey,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
}

impl ActivateLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
            }),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
