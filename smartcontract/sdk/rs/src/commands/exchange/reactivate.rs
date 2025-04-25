use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_exchange_pda,
    processors::exchange::reactivate::ExchangeReactivateArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

pub struct ReactivateExchangeCommand {
    pub index: u128,
}

impl ReactivateExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) = get_exchange_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::ReactivateExchange(ExchangeReactivateArgs { index: self.index }),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
