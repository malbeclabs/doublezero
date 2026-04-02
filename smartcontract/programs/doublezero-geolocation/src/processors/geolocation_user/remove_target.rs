use crate::{
    error::GeolocationError,
    processors::check_foundation_allowlist,
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
    let program_config_account = next_account_info(accounts_iter)?;
    let serviceability_globalstate_account = next_account_info(accounts_iter)?;
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
        check_foundation_allowlist(
            program_config_account,
            serviceability_globalstate_account,
            payer_account,
            program_id,
        )?;
    }

    let mut probe = GeoProbe::try_from(probe_account)?;

    let geoprobe_pk = *probe_account.key;

    let index = user
        .targets
        .iter()
        .position(|t| {
            t.target_type == args.target_type
                && t.geoprobe_pk == geoprobe_pk
                && match args.target_type {
                    GeoLocationTargetType::Outbound | GeoLocationTargetType::OutboundIcmp => {
                        t.ip_address == args.ip_address
                    }
                    GeoLocationTargetType::Inbound => t.target_pk == args.target_pk,
                }
        })
        .ok_or(GeolocationError::TargetNotFound)?;

    user.targets.swap_remove(index);

    probe.reference_count = probe.reference_count.saturating_sub(1);
    probe.target_update_count = probe.target_update_count.wrapping_add(1);

    try_acc_write(&user, user_account, payer_account, accounts)?;
    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
