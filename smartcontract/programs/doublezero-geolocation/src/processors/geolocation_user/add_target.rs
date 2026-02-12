use crate::{
    error::GeolocationError,
    instructions::AddTargetArgs,
    serializer::try_acc_write,
    state::geolocation_user::{GeolocationTarget, GeolocationUser, MAX_TARGETS},
    validation::validate_public_ip,
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

pub fn process_add_target(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &AddTargetArgs,
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

    user.targets.push(GeolocationTarget {
        target_ip: args.target_ip,
        target_port: args.target_port,
        probe_pk: args.probe_pk,
    });

    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}
