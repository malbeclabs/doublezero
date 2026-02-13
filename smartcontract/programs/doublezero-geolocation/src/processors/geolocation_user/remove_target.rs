use crate::{
    error::GeolocationError,
    instructions::RemoveTargetArgs,
    serializer::try_acc_write,
    state::{geo_probe::GeoProbe, geolocation_user::GeolocationUser},
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

pub fn process_remove_target(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &RemoveTargetArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let probe_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;

    if !payer_account.is_signer {
        msg!("Payer must be a signer");
        return Err(ProgramError::MissingRequiredSignature);
    }
    if user_account.owner != program_id {
        msg!("Invalid GeolocationUser Account Owner");
        return Err(ProgramError::IllegalOwner);
    }
    if probe_account.owner != program_id {
        msg!("Invalid GeoProbe Account Owner");
        return Err(ProgramError::IllegalOwner);
    }
    if probe_account.key != &args.probe_pk {
        msg!("Probe account does not match probe_pk in args");
        return Err(ProgramError::InvalidAccountData);
    }

    let mut user = GeolocationUser::try_from(user_account)?;

    if user.owner != *payer_account.key {
        return Err(GeolocationError::InvalidOwner.into());
    }

    let pos = user.targets.iter().position(|t| {
        t.target_ip == args.target_ip
            && t.target_port == args.target_port
            && t.probe_pk == args.probe_pk
    });

    match pos {
        Some(index) => {
            user.targets.swap_remove(index);
        }
        None => {
            msg!(
                "Target not found: {}:{} probe={}",
                args.target_ip,
                args.target_port,
                args.probe_pk
            );
            return Err(GeolocationError::TargetNotFound.into());
        }
    }

    let mut probe = GeoProbe::try_from(probe_account)?;
    probe.reference_count = probe.reference_count.saturating_sub(1);

    try_acc_write(&user, user_account, payer_account, accounts)?;
    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
