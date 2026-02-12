pub mod geo_probe;
pub mod geolocation_user;
pub mod program_config;

use crate::{error::GeolocationError, state::program_config::GeolocationProgramConfig};
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, msg, pubkey::Pubkey};

pub fn check_foundation_allowlist(
    program_config_account: &AccountInfo,
    serviceability_globalstate_account: &AccountInfo,
    payer_account: &AccountInfo,
    program_id: &Pubkey,
) -> ProgramResult {
    assert_eq!(
        program_config_account.owner, program_id,
        "Invalid ProgramConfig Account Owner"
    );

    let program_config = GeolocationProgramConfig::try_from(program_config_account)?;

    if *serviceability_globalstate_account.owner != program_config.serviceability_program_id {
        msg!(
            "Expected serviceability program: {}, got: {}",
            program_config.serviceability_program_id,
            serviceability_globalstate_account.owner
        );
        return Err(GeolocationError::InvalidServiceabilityProgramId.into());
    }

    let globalstate = doublezero_serviceability::state::globalstate::GlobalState::try_from(
        serviceability_globalstate_account,
    )?;

    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(GeolocationError::NotAllowed.into());
    }

    Ok(())
}
