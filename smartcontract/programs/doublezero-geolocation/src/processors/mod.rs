pub mod program_config;

use crate::{
    error::GeolocationError, pda::get_program_config_pda,
    state::program_config::GeolocationProgramConfig,
};
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

    // Verify ProgramConfig PDA address
    let (expected_config_pda, _) = get_program_config_pda(program_id);
    if program_config_account.key != &expected_config_pda {
        msg!("Invalid ProgramConfig PDA");
        return Err(ProgramError::InvalidSeeds);
    }

    let program_config = GeolocationProgramConfig::try_from(program_config_account)?;

    if *serviceability_globalstate_account.owner != program_config.serviceability_program_id {
        msg!(
            "Expected serviceability program: {}, got: {}",
            program_config.serviceability_program_id,
            serviceability_globalstate_account.owner
        );
        return Err(GeolocationError::InvalidServiceabilityProgramId.into());
    }

    // Verify serviceability GlobalState PDA address
    let (expected_gs_pda, _) = doublezero_serviceability::pda::get_globalstate_pda(
        &program_config.serviceability_program_id,
    );
    if serviceability_globalstate_account.key != &expected_gs_pda {
        msg!("Invalid Serviceability GlobalState PDA");
        return Err(ProgramError::InvalidSeeds);
    }

    let globalstate = doublezero_serviceability::state::globalstate::GlobalState::try_from(
        serviceability_globalstate_account,
    )?;

    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(GeolocationError::NotAllowed.into());
    }

    Ok(program_config)
}
