use crate::{
    error::GeolocationError,
    instructions::AddParentDeviceArgs,
    processors::check_foundation_allowlist,
    serializer::try_acc_write,
    state::geo_probe::{GeoProbe, MAX_PARENT_DEVICES},
};
use doublezero_serviceability::state::device::{Device, DeviceStatus};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

pub fn process_add_parent_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &AddParentDeviceArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let probe_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let program_config_account = next_account_info(accounts_iter)?;
    let serviceability_globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    if !payer_account.is_signer {
        msg!("Payer must be a signer");
        return Err(ProgramError::MissingRequiredSignature);
    }

    let program_config = check_foundation_allowlist(
        program_config_account,
        serviceability_globalstate_account,
        payer_account,
        program_id,
    )?;

    // Validate device_account key matches the requested device_pk
    if device_account.key != &args.device_pk {
        msg!(
            "Device account key {} does not match args.device_pk {}",
            device_account.key,
            args.device_pk
        );
        return Err(ProgramError::InvalidAccountData);
    }

    // Validate device_account belongs to the Serviceability program
    if *device_account.owner != program_config.serviceability_program_id {
        msg!(
            "Device account owner {} does not match serviceability program {}",
            device_account.owner,
            program_config.serviceability_program_id
        );
        return Err(GeolocationError::InvalidServiceabilityProgramId.into());
    }

    // Verify it's a valid, activated Device account
    let device = Device::try_from(device_account)?;
    if device.status != DeviceStatus::Activated {
        msg!(
            "Device {} is not activated (status: {:?})",
            device_account.key,
            device.status
        );
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

    let mut probe = GeoProbe::try_from(probe_account)?;

    if probe.parent_devices.len() >= MAX_PARENT_DEVICES {
        msg!(
            "Max parent devices reached: {} (max {})",
            probe.parent_devices.len(),
            MAX_PARENT_DEVICES
        );
        return Err(GeolocationError::MaxParentDevicesReached.into());
    }

    if probe.parent_devices.contains(&args.device_pk) {
        msg!("Parent device already exists: {}", args.device_pk);
        return Err(GeolocationError::ParentDeviceAlreadyExists.into());
    }

    probe.parent_devices.push(args.device_pk);

    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
