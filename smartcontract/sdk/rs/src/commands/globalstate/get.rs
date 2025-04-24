use double_zero_sla_program::{pda::get_globalstate_pda, state::{accountdata::AccountData, globalstate::GlobalState}};
use solana_sdk::pubkey::Pubkey;
use eyre::eyre;

use crate::DoubleZeroClient;

pub struct GetGlobalStateCommand {}

impl GetGlobalStateCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, GlobalState)> {
        let (pubkey, _) = get_globalstate_pda(&client.get_program_id());

        match client.get(pubkey)? {
            AccountData::GlobalState(globalstate) => Ok((pubkey, globalstate)),
            _ => Err(eyre!("Invalid global state")),
        }
    }
}
