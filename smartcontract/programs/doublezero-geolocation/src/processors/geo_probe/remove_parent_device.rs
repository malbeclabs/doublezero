use crate::{
    error::GeolocationError, instructions::RemoveParentDeviceArgs,
    processors::check_foundation_allowlist, serializer::try_acc_write, state::geo_probe::GeoProbe,
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

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

    assert!(payer_account.is_signer, "Payer must be a signer");

    check_foundation_allowlist(
        program_config_account,
        serviceability_globalstate_account,
        payer_account,
        program_id,
    )?;

    assert_eq!(
        probe_account.owner, program_id,
        "Invalid GeoProbe Account Owner"
    );

    let mut probe = GeoProbe::try_from(probe_account)?;

    let pos = probe
        .parent_devices
        .iter()
        .position(|pk| *pk == args.device_pk);

    match pos {
        Some(index) => {
            probe.parent_devices.swap_remove(index);
        }
        None => {
            msg!("Parent device not found: {}", args.device_pk);
            return Err(GeolocationError::ParentDeviceNotFound.into());
        }
    }

    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
