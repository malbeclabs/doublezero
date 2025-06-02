use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_tunnel_pda,
    processors::link::reject::LinkRejectArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct RejectLinkCommand {
    pub index: u128,
    pub reason: String,
}

impl RejectLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) = get_tunnel_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::RejectLink(LinkRejectArgs {
                index: self.index,
                bump_seed,
                reason: self.reason.clone(),
            }),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
