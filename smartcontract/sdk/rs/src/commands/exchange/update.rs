use double_zero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_exchange_pda,
    processors::exchange::update::ExchangeUpdateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::accountdata::getglobalstate::GetGlobalStateCommand, DoubleZeroClient};

pub struct UpdateExchangeCommand {
    pub index: u128,
    pub code: Option<String>,
    pub name: Option<String>,
    pub lat: Option<f64>,
    pub lng: Option<f64>,
    pub loc_id: Option<u32>,
}

impl UpdateExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) =
            get_exchange_pda(&client.get_program_id(), self.index);
        client
            .execute_transaction(
                DoubleZeroInstruction::UpdateExchange(ExchangeUpdateArgs {
                    index: self.index,
                    code: self.code.to_owned(),
                    name: self.name.to_owned(),
                    lat: self.lat,
                    lng: self.lng,
                    loc_id: self.loc_id,
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}
