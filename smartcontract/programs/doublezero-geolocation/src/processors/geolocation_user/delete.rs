use crate::{
    error::GeolocationError, processors::check_foundation_allowlist, serializer::try_acc_close,
    state::geolocation_user_view::GeolocationUserView,
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

pub fn process_delete_geolocation_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let program_config_account = next_account_info(accounts_iter)?;
    let serviceability_globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;

    if !payer_account.is_signer {
        msg!("Payer must be a signer");
        return Err(ProgramError::MissingRequiredSignature);
    }

    if user_account.owner != program_id {
        msg!("Invalid GeolocationUser Account Owner");
        return Err(ProgramError::IllegalOwner);
    }
    if !user_account.is_writable {
        msg!("GeolocationUser account must be writable");
        return Err(ProgramError::InvalidAccountData);
    }

    let view = GeolocationUserView::try_from_account(user_account)?;

    if view.owner != *payer_account.key {
        check_foundation_allowlist(
            program_config_account,
            serviceability_globalstate_account,
            payer_account,
            program_id,
        )?;
    }

    if view.targets_count != 0 {
        msg!(
            "Cannot delete GeolocationUser with {} remaining targets",
            view.targets_count
        );
        return Err(GeolocationError::TargetsNotEmpty.into());
    }

    try_acc_close(user_account, payer_account)?;

    Ok(())
}
