use crate::{
    instructions::UpdatePaymentStatusArgs,
    processors::check_foundation_allowlist,
    serializer::try_acc_write,
    state::geolocation_user::{
        GeolocationBillingConfig, GeolocationPaymentStatus, GeolocationUser,
    },
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
    let _system_program = next_account_info(accounts_iter)?;

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
    if !user_account.is_writable {
        msg!("GeolocationUser account must be writable");
        return Err(ProgramError::InvalidAccountData);
    }

    let mut user = GeolocationUser::try_from(user_account)?;

    user.payment_status = GeolocationPaymentStatus::try_from(args.payment_status)?;

    if let Some(epoch) = args.last_deduction_dz_epoch {
        let GeolocationBillingConfig::FlatPerEpoch(ref mut config) = user.billing;
        config.last_deduction_dz_epoch = epoch;
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}
