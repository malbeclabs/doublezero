pub mod init;
pub mod update;

use solana_program::{program_error::ProgramError, pubkey::Pubkey};

pub(crate) fn parse_upgrade_authority(data: &[u8]) -> Result<Option<Pubkey>, ProgramError> {
    const PROGRAM_DATA_DISCRIMINANT: u32 = 3;
    const MIN_LEN: usize = 4 + 8 + 1; // discriminant + slot + option tag

    if data.len() < MIN_LEN {
        return Err(ProgramError::InvalidAccountData);
    }

    let discriminant = u32::from_le_bytes(
        data[0..4]
            .try_into()
            .map_err(|_| ProgramError::InvalidAccountData)?,
    );
    if discriminant != PROGRAM_DATA_DISCRIMINANT {
        return Err(ProgramError::InvalidAccountData);
    }

    match data[12] {
        0 => Ok(None),
        1 => {
            if data.len() < MIN_LEN + 32 {
                return Err(ProgramError::InvalidAccountData);
            }
            let authority =
                Pubkey::try_from(&data[13..45]).map_err(|_| ProgramError::InvalidAccountData)?;
            Ok(Some(authority))
        }
        _ => Err(ProgramError::InvalidAccountData),
    }
}
