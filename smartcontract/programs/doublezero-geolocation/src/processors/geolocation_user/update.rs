use crate::{
    error::GeolocationError, state::geolocation_user_view::GeolocationUserView,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, Debug, Default, PartialEq, Clone)]
pub struct UpdateGeolocationUserArgs {
    pub token_account: Option<Pubkey>,
}

pub fn process_update_geolocation_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &UpdateGeolocationUserArgs,
) -> ProgramResult {
    if *args == UpdateGeolocationUserArgs::default() {
        msg!("No-op Update, Skipping");
        return Err(ProgramError::InvalidInstructionData);
    }

    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
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

    let mut view = GeolocationUserView::try_from_account(user_account)?;

    if view.owner != *payer_account.key {
        msg!("Signer is not the account owner");
        return Err(GeolocationError::Unauthorized.into());
    }

    if let Some(token_account) = args.token_account {
        view.token_account = token_account;
    }

    view.write_prefix(user_account)?;

    Ok(())
}
