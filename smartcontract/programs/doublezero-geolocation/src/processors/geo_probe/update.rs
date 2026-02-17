use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use crate::{
    processors::check_foundation_allowlist,
    serializer::try_acc_write,
    state::geo_probe::GeoProbe,
    validation::validate_public_ip,
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
pub struct UpdateGeoProbeArgs {
    pub public_ip: Option<Ipv4Addr>,
    pub location_offset_port: Option<u16>,
    pub metrics_publisher_pk: Option<Pubkey>,
}

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

    if let Some(ref public_ip) = args.public_ip {
        validate_public_ip(public_ip)?;
        probe.public_ip = *public_ip;
    }
    if let Some(location_offset_port) = args.location_offset_port {
        probe.location_offset_port = location_offset_port;
    }
    if let Some(metrics_publisher_pk) = args.metrics_publisher_pk {
        probe.metrics_publisher_pk = metrics_publisher_pk;
    }

    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
