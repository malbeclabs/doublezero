use crate::{
    error::GeolocationError,
    serializer::try_acc_write,
    state::{
        geo_probe::GeoProbe,
        geolocation_user::{GeoLocationTargetType, GeolocationTarget, MAX_TARGETS},
        geolocation_user_view::GeolocationUserView,
    },
    validation::validate_public_ip,
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
pub struct AddTargetArgs {
    pub target_type: GeoLocationTargetType,
    pub ip_address: Ipv4Addr,
    pub location_offset_port: u16,
    pub target_pk: Pubkey,
}

pub fn process_add_target(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &AddTargetArgs,
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

    let mut view = GeolocationUserView::try_from_account(user_account)?;

    if view.owner != *payer_account.key {
        msg!("Signer is not the account owner");
        return Err(GeolocationError::Unauthorized.into());
    }

    let mut probe = GeoProbe::try_from(probe_account)?;

    if (view.targets_count as usize) >= MAX_TARGETS {
        msg!("Cannot add target: already at maximum of {}", MAX_TARGETS);
        return Err(GeolocationError::MaxTargetsReached.into());
    }

    let geoprobe_pk = *probe_account.key;

    match args.target_type {
        GeoLocationTargetType::Outbound | GeoLocationTargetType::OutboundIcmp => {
            validate_public_ip(&args.ip_address)?;
        }
        GeoLocationTargetType::Inbound => {
            if args.target_pk == Pubkey::default() {
                msg!("Inbound target requires a non-default target_pk");
                return Err(ProgramError::InvalidInstructionData);
            }
        }
    }

    // Duplicate check: scan the targets section without materializing it.
    {
        let data = user_account.try_borrow_data()?;
        let cursor = view.cursor(&data)?;
        let duplicate = cursor.find_match(|t| {
            t.target_type == args.target_type
                && t.geoprobe_pk == geoprobe_pk
                && t.ip_address == args.ip_address
                && t.target_pk == args.target_pk
        })?;
        if duplicate.is_some() {
            msg!("Target already exists");
            return Err(GeolocationError::TargetAlreadyExists.into());
        }
    }

    let new_target = GeolocationTarget {
        target_type: args.target_type,
        ip_address: args.ip_address,
        location_offset_port: args.location_offset_port,
        target_pk: args.target_pk,
        geoprobe_pk,
    };
    view.append_target(user_account, payer_account, accounts, &new_target)?;

    probe.reference_count = probe.reference_count.saturating_add(1);
    probe.target_update_count = probe.target_update_count.wrapping_add(1);

    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
