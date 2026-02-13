use crate::{
    error::GeolocationError,
    instructions::AddTargetArgs,
    serializer::try_acc_write,
    state::{
        geo_probe::GeoProbe,
        geolocation_user::{GeolocationTarget, GeolocationUser, MAX_TARGETS},
    },
    validation::validate_public_ip,
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

pub fn process_add_target(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &AddTargetArgs,
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

    if user.targets.len() >= MAX_TARGETS {
        msg!(
            "Max targets reached: {} (max {})",
            user.targets.len(),
            MAX_TARGETS
        );
        return Err(GeolocationError::MaxTargetsReached.into());
    }

    validate_public_ip(&args.target_ip)?;

    let already_exists = user.targets.iter().any(|t| {
        t.target_ip == args.target_ip
            && t.target_port == args.target_port
            && t.probe_pk == args.probe_pk
    });
    if already_exists {
        msg!(
            "Target already exists: {}:{} probe={}",
            args.target_ip,
            args.target_port,
            args.probe_pk
        );
        return Err(GeolocationError::TargetAlreadyExists.into());
    }

    let mut probe = GeoProbe::try_from(probe_account)?;
    probe.reference_count = probe
        .reference_count
        .checked_add(1)
        .ok_or(GeolocationError::ReferenceCountOverflow)?;

    user.targets.push(GeolocationTarget {
        target_ip: args.target_ip,
        target_port: args.target_port,
        probe_pk: args.probe_pk,
    });

    try_acc_write(&user, user_account, payer_account, accounts)?;
    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
