use double_zero_sla_program::{pda::get_globalstate_pda, state::accountdata::AccountData};
use solana_sdk::pubkey::Pubkey;
use eyre::eyre;

use crate::DoubleZeroClient;

pub struct ListUserAllowlistCommand {}

impl ListUserAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<Pubkey>> {
        let (pubkey, _) = get_globalstate_pda(&client.get_program_id());

        match client.get(pubkey)? {
            AccountData::GlobalState(globalstate) => Ok(globalstate.user_allowlist),
            _ => Err(eyre!("Invalid global state")),
        }
    }
}
