use crate::{
    error::GeolocationError,
    instructions::AddParentDeviceArgs,
    processors::check_foundation_allowlist,
    serializer::try_acc_write,
    state::geo_probe::{GeoProbe, MAX_PARENT_DEVICES},
};
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
    let program_config_account = next_account_info(accounts_iter)?;
    let serviceability_globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;

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
