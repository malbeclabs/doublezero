use double_zero_sla_program::{pda::get_globalstate_pda, state::accountdata::AccountData};
use solana_sdk::pubkey::Pubkey;
use eyre::eyre;

use crate::DoubleZeroClient;

pub struct ListFoundationAllowlistCommand {}

impl ListFoundationAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<Pubkey>> {
        let (pubkey, _) = get_globalstate_pda(&client.get_program_id());

        match client.get(pubkey)? {
            AccountData::GlobalState(globalstate) => Ok(globalstate.foundation_allowlist),
            _ => Err(eyre!("Invalid global state")),
        }
    }
}
