use crate::{
    error::GeolocationError, instructions::UpdateGeolocationUserArgs, serializer::try_acc_write,
    state::geolocation_user::GeolocationUser,
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

pub fn process_update_geolocation_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &UpdateGeolocationUserArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

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

    let mut user = GeolocationUser::try_from(user_account)?;

    if user.owner != *payer_account.key {
        return Err(GeolocationError::InvalidOwner.into());
    }

    if let Some(token_account) = args.token_account {
        user.token_account = token_account;
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}
