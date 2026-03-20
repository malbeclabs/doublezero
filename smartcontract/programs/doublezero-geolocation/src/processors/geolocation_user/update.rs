use crate::{
    error::GeolocationError, serializer::try_acc_write, state::geolocation_user::GeolocationUser,
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

    let mut user = GeolocationUser::try_from(user_account)?;

    if user.owner != *payer_account.key {
        msg!("Signer is not the account owner");
        return Err(GeolocationError::Unauthorized.into());
    }

    // update_count is intentionally not incremented here. It tracks changes to
    // probe-relevant state (targets, payment_status). token_account is billing
    // plumbing and does not affect geoProbe polling.
    if let Some(token_account) = args.token_account {
        user.token_account = token_account;
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}
