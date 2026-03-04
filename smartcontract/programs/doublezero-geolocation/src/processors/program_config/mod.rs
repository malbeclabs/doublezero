pub mod init;
pub mod update;

use solana_bincode::limited_deserialize;
use solana_loader_v3_interface::state::UpgradeableLoaderState;
use solana_program::{program_error::ProgramError, pubkey::Pubkey};

pub(crate) fn parse_upgrade_authority(data: &[u8]) -> Result<Option<Pubkey>, ProgramError> {
    let state: UpgradeableLoaderState = limited_deserialize(
        data,
        UpgradeableLoaderState::size_of_programdata_metadata() as u64,
    )
    .map_err(|_| ProgramError::InvalidAccountData)?;
    match state {
        UpgradeableLoaderState::ProgramData {
            upgrade_authority_address,
            ..
        } => Ok(upgrade_authority_address),
        _ => Err(ProgramError::InvalidAccountData),
    }
}
