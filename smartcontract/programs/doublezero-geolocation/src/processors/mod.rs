pub mod geo_probe;
pub mod program_config;

use crate::{error::GeolocationError, state::program_config::GeolocationProgramConfig};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};

pub fn check_foundation_allowlist(
    program_config_account: &AccountInfo,
    serviceability_globalstate_account: &AccountInfo,
    payer_account: &AccountInfo,
    program_id: &Pubkey,
) -> Result<GeolocationProgramConfig, ProgramError> {
    if program_config_account.owner != program_id {
        msg!("Invalid ProgramConfig Account Owner");
        return Err(ProgramError::IllegalOwner);
    }

    let program_config = GeolocationProgramConfig::try_from(program_config_account)?;

    let serviceability_program_id = &crate::serviceability_program_id();
    if serviceability_globalstate_account.owner != serviceability_program_id {
        msg!(
            "Expected serviceability program: {}, got: {}",
            serviceability_program_id,
            serviceability_globalstate_account.owner
        );
        return Err(ProgramError::IncorrectProgramId);
    }

    let globalstate = doublezero_serviceability::state::globalstate::GlobalState::try_from(
        serviceability_globalstate_account,
    )?;

    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(GeolocationError::NotAllowed.into());
    }

    Ok(program_config)
}
