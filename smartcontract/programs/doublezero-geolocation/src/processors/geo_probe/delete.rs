use crate::{
    error::GeolocationError, processors::check_foundation_allowlist, serializer::try_acc_close,
    state::geo_probe::GeoProbe,
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

pub fn process_delete_geo_probe(program_id: &Pubkey, accounts: &[AccountInfo]) -> ProgramResult {
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

    let probe = GeoProbe::try_from(probe_account)?;

    if probe.reference_count > 0 {
        msg!(
            "Cannot delete GeoProbe with reference_count={}",
            probe.reference_count
        );
        return Err(GeolocationError::ReferenceCountNotZero.into());
    }

    try_acc_close(probe_account, payer_account)?;

    Ok(())
}
