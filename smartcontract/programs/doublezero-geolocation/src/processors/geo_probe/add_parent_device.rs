use crate::{
    error::GeolocationError, processors::check_foundation_allowlist, serializer::try_acc_write,
    state::geo_probe::GeoProbe,
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

pub fn process_add_parent_device(program_id: &Pubkey, accounts: &[AccountInfo]) -> ProgramResult {
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

    // Validate device_account belongs to the Serviceability program
    let serviceability_program_id = crate::serviceability_program_id();
    if *device_account.owner != serviceability_program_id {
        msg!(
            "Device account owner {} does not match serviceability program {}",
            device_account.owner,
            serviceability_program_id
        );
        return Err(GeolocationError::InvalidServiceabilityProgramId.into());
    }

    // Verify it's a valid, activated Device
    let device = doublezero_serviceability::state::device::Device::try_from(device_account)?;
    if device.status != doublezero_serviceability::state::device::DeviceStatus::Activated {
        msg!(
            "Device {} is not activated (status: {:?})",
            device_account.key,
            device.status
        );
        return Err(ProgramError::InvalidAccountData);
    }

    let mut probe = GeoProbe::try_from(probe_account)?;

    if probe.parent_devices.contains(device_account.key) {
        msg!("Device {} is already a parent device", device_account.key);
        return Err(GeolocationError::ParentDeviceAlreadyExists.into());
    }

    // MAX_PARENT_DEVICES is enforced by probe.validate() inside try_acc_write
    probe.parent_devices.push(*device_account.key);

    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
