use doublezero_sla_program::{
    pda::get_globalconfig_pda,
    state::{accountdata::AccountData, globalconfig::GlobalConfig},
};
use eyre::eyre;
use solana_sdk::pubkey::Pubkey;

use crate::DoubleZeroClient;

#[derive(Default, Debug, PartialEq, Clone)]
pub struct GetGlobalConfigCommand;

impl GetGlobalConfigCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, GlobalConfig)> {
        let (pubkey, _) = get_globalconfig_pda(&client.get_program_id());

        match client.get(pubkey)? {
            AccountData::GlobalConfig(globalstate) => Ok((pubkey, globalstate)),
            _ => Err(eyre!("Invalid global state")),
        }
    }
}
