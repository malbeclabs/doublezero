use doublezero_serviceability::{
    pda::get_program_config_pda,
    state::{accountdata::AccountData, programconfig::ProgramConfig},
};
use eyre::eyre;
use solana_sdk::pubkey::Pubkey;

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct GetProgramConfigCommand;

impl GetProgramConfigCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, ProgramConfig)> {
        let (pubkey, _) = get_program_config_pda(&client.get_program_id());

        match client.get(pubkey)? {
            AccountData::ProgramConfig(config) => Ok((pubkey, config)),
            _ => Err(eyre!("Invalid global state")),
        }
    }
}
