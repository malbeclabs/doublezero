use crate::{
    error::GeolocationError,
    instructions::UpdateGeolocationUserArgs,
    serializer::try_acc_write,
    state::geolocation_user::{GeolocationUser, GeolocationUserStatus},
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
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

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(
        user_account.owner, program_id,
        "Invalid GeolocationUser Account Owner"
    );

    let mut user = GeolocationUser::try_from(user_account)?;

    if user.owner != *payer_account.key {
        return Err(GeolocationError::InvalidOwner.into());
    }

    if let Some(token_account) = args.token_account {
        user.token_account = token_account;
    }
    if let Some(status) = args.status {
        user.status = GeolocationUserStatus::from(status);
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}
