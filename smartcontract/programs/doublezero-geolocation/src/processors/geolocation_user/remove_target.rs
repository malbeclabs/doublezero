use crate::{
    error::GeolocationError,
    serializer::try_acc_write,
    state::{
        geo_probe::GeoProbe,
        geolocation_user::{GeoLocationTargetType, GeolocationUser},
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct RemoveTargetArgs {
    pub target_type: GeoLocationTargetType,
    pub ip_address: Ipv4Addr,
    pub target_pk: Pubkey,
}

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
    if !user_account.is_writable {
        msg!("GeolocationUser account must be writable");
        return Err(ProgramError::InvalidAccountData);
    }

    if probe_account.owner != program_id {
        msg!("Invalid GeoProbe Account Owner");
        return Err(ProgramError::IllegalOwner);
    }
    if !probe_account.is_writable {
        msg!("GeoProbe account must be writable");
        return Err(ProgramError::InvalidAccountData);
    }

    let mut user = GeolocationUser::try_from(user_account)?;

    if user.owner != *payer_account.key {
        msg!("Signer is not the account owner");
        return Err(GeolocationError::Unauthorized.into());
    }

    let mut probe = GeoProbe::try_from(probe_account)?;

    let geoprobe_pk = *probe_account.key;
    let original_len = user.targets.len();

    user.targets.retain(|t| {
        !(t.target_type == args.target_type
            && t.geoprobe_pk == geoprobe_pk
            && t.ip_address == args.ip_address
            && t.target_pk == args.target_pk)
    });

    if user.targets.len() == original_len {
        msg!("Target not found");
        return Err(GeolocationError::TargetNotFound.into());
    }

    probe.reference_count = probe.reference_count.saturating_sub(1);

    try_acc_write(&user, user_account, payer_account, accounts)?;
    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
