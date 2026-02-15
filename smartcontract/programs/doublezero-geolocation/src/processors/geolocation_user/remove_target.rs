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
    let _system_program = next_account_info(accounts_iter)?;

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
    if !user_account.is_writable {
        msg!("GeolocationUser account must be writable");
        return Err(ProgramError::InvalidAccountData);
    }
    if !probe_account.is_writable {
        msg!("GeoProbe account must be writable");
        return Err(ProgramError::InvalidAccountData);
    }

    let mut probe = GeoProbe::try_from(probe_account)?;

    if probe.exchange_pk != args.exchange_pk {
        msg!("Probe exchange_pk does not match exchange_pk in args");
        return Err(ProgramError::InvalidAccountData);
    }

    let mut user = GeolocationUser::try_from(user_account)?;

    if user.owner != *payer_account.key {
        return Err(GeolocationError::InvalidOwner.into());
    }

    let pos = user
        .targets
        .iter()
        .position(|t| t.ip_address == args.target_ip && t.geoprobe_pk == *probe_account.key);

    match pos {
        Some(index) => {
            user.targets.swap_remove(index);
        }
        None => {
            msg!(
                "Target not found: {} probe={}",
                args.target_ip,
                probe_account.key
            );
            return Err(GeolocationError::TargetNotFound.into());
        }
    }

    probe.reference_count = probe.reference_count.saturating_sub(1);

    try_acc_write(&user, user_account, payer_account, accounts)?;
    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
