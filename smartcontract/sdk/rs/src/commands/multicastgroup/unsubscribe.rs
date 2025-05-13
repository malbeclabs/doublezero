use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_multicastgroup_pda,
    processors::multicastgroup::unsubscribe::MulticastGroupUnsubscribeArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct UnsubscribeMulticastGroupCommand {
    pub index: u128,
    pub publishers: Vec<Pubkey>,
    pub subscribers: Vec<Pubkey>,
}

impl UnsubscribeMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) = get_multicastgroup_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::UnsubscribeMulticastGroup(MulticastGroupUnsubscribeArgs {
                index: self.index,
                bump_seed,
                publishers: self.publishers.clone(),
                subscribers: self.subscribers.clone(),
            }),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
