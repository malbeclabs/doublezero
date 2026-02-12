use crate::{
    instructions::UpdateGeoProbeArgs, processors::check_foundation_allowlist,
    serializer::try_acc_write, state::geo_probe::GeoProbe, validation::validate_public_ip,
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

pub fn process_update_geo_probe(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &UpdateGeoProbeArgs,
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

    if let Some(ref public_ip) = args.public_ip {
        validate_public_ip(public_ip)?;
        probe.public_ip = *public_ip;
    }
    if let Some(port) = args.port {
        probe.port = port;
    }
    if let Some(metrics_publisher_pk) = args.metrics_publisher_pk {
        probe.metrics_publisher_pk = metrics_publisher_pk;
    }
    if let Some(latency_threshold_ns) = args.latency_threshold_ns {
        probe.latency_threshold_ns = latency_threshold_ns;
    }

    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
