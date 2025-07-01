use doublezero_serviceability::{
    pda::get_globalstate_pda,
    state::{accountdata::AccountData, globalstate::GlobalState},
};
use eyre::eyre;
use solana_sdk::pubkey::Pubkey;

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct GetGlobalStateCommand;

impl GetGlobalStateCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, GlobalState)> {
        let (pubkey, _) = get_globalstate_pda(&client.get_program_id());

        match client.get(pubkey)? {
            AccountData::GlobalState(globalstate) => Ok((pubkey, globalstate)),
            _ => Err(eyre!("Invalid global state")),
        }
    }
}
