use crate::{
    instructions::UpdatePaymentStatusArgs,
    processors::check_foundation_allowlist,
    serializer::try_acc_write,
    state::geolocation_user::{GeolocationUser, PaymentStatus},
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

pub fn process_update_payment_status(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &UpdatePaymentStatusArgs,
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

    check_foundation_allowlist(
        program_config_account,
        serviceability_globalstate_account,
        payer_account,
        program_id,
    )?;

    if user_account.owner != program_id {
        msg!("Invalid GeolocationUser Account Owner");
        return Err(ProgramError::IllegalOwner);
    }

    let mut user = GeolocationUser::try_from(user_account)?;

    user.payment_status = PaymentStatus::try_from(args.payment_status)?;

    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}
