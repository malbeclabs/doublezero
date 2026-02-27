use crate::{
    error::GeolocationError, processors::check_foundation_allowlist, serializer::try_acc_write,
    state::geo_probe::GeoProbe,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct RemoveParentDeviceArgs {
    pub device_pk: Pubkey,
}

pub fn process_remove_parent_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &RemoveParentDeviceArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let probe_account = next_account_info(accounts_iter)?;
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

    if probe_account.owner != program_id {
        msg!("Invalid GeoProbe Account Owner");
        return Err(ProgramError::IllegalOwner);
    }
    if !probe_account.is_writable {
        msg!("GeoProbe account must be writable");
        return Err(ProgramError::InvalidAccountData);
    }

    let mut probe = GeoProbe::try_from(probe_account)?;

    if !probe.parent_devices.contains(&args.device_pk) {
        msg!("Device {} is not a parent device", args.device_pk);
        return Err(GeolocationError::ParentDeviceNotFound.into());
    }

    probe.parent_devices.retain(|&pk| pk != args.device_pk);

    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
