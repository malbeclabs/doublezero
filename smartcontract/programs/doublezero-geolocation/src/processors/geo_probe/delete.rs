use crate::{
    error::GeolocationError, processors::check_foundation_allowlist, serializer::try_acc_close,
    state::geo_probe::GeoProbe,
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

pub fn process_delete_geo_probe(program_id: &Pubkey, accounts: &[AccountInfo]) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let probe_account = next_account_info(accounts_iter)?;
    let program_config_account = next_account_info(accounts_iter)?;
    let serviceability_globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;

    if !payer_account.is_signer {
        msg!("Payer must be a signer");
        return Err(ProgramError::MissingRequiredSignature);
    }

    check_foundation_allowlist(
        program_config_account,
        serviceability_globalstate_account,
        payer_account,
        program_id,
    )?;

    if probe_account.owner != program_id {
        msg!("Invalid GeoProbe Account Owner");
        return Err(ProgramError::IllegalOwner);
    }
    if !probe_account.is_writable {
        msg!("GeoProbe account must be writable");
        return Err(ProgramError::InvalidAccountData);
    }

    let probe = GeoProbe::try_from(probe_account)?;

    if probe.reference_count > 0 {
        msg!(
            "Cannot delete GeoProbe with reference_count={}",
            probe.reference_count
        );
        return Err(GeolocationError::ReferenceCountNotZero.into());
    }

    try_acc_close(probe_account, payer_account)?;

    Ok(())
}
