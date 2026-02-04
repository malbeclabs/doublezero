use doublezero_serviceability::{pda::get_globalstate_pda, state::accountdata::AccountData};
use eyre::eyre;
use solana_sdk::pubkey::Pubkey;

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct ListQaAllowlistCommand;

impl ListQaAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<Pubkey>> {
        let (pubkey, _) = get_globalstate_pda(&client.get_program_id());

        match client.get(pubkey)? {
            AccountData::GlobalState(globalstate) => Ok(globalstate.qa_allowlist),
            _ => Err(eyre!("Invalid global state")),
        }
    }
}
