use crate::{
    error::GeolocationError, serializer::try_acc_close, state::geolocation_user::GeolocationUser,
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

pub fn process_delete_geolocation_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(
        user_account.owner, program_id,
        "Invalid GeolocationUser Account Owner"
    );

    let user = GeolocationUser::try_from(user_account)?;

    if user.owner != *payer_account.key {
        return Err(GeolocationError::InvalidOwner.into());
    }

    if !user.targets.is_empty() {
        msg!(
            "Cannot delete user with {} remaining targets",
            user.targets.len()
        );
        return Err(GeolocationError::TargetsNotEmpty.into());
    }

    try_acc_close(user_account, payer_account)?;

    Ok(())
}
