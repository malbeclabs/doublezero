use crate::{
    error::GeolocationError,
    pda::get_geo_probe_pda,
    processors::check_foundation_allowlist,
    seeds::{SEED_PREFIX, SEED_PROBE},
    serializer::try_acc_create,
    state::{accounttype::AccountType, geo_probe::GeoProbe},
    validation::{validate_code_length, validate_public_ip},
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::validate_account_code;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct CreateGeoProbeArgs {
    pub code: String,
    pub public_ip: Ipv4Addr,
    pub location_offset_port: u16,
    pub metrics_publisher_pk: Pubkey,
}

pub fn process_create_geo_probe(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &CreateGeoProbeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let probe_account = next_account_info(accounts_iter)?;
    let exchange_account = next_account_info(accounts_iter)?;
    let program_config_account = next_account_info(accounts_iter)?;
    let serviceability_globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

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

    // Validate exchange_account belongs to the Serviceability program
    let serviceability_program_id = crate::serviceability_program_id();
    if *exchange_account.owner != serviceability_program_id {
        msg!(
            "Exchange account owner {} does not match serviceability program {}",
            exchange_account.owner,
            serviceability_program_id
        );
        return Err(GeolocationError::InvalidServiceabilityProgramId.into());
    }

    // Verify it's a valid, activated Exchange account
    let exchange =
        doublezero_serviceability::state::exchange::Exchange::try_from(exchange_account)?;
    if exchange.status != doublezero_serviceability::state::exchange::ExchangeStatus::Activated {
        msg!(
            "Exchange {} is not activated (status: {:?})",
            exchange_account.key,
            exchange.status
        );
        return Err(ProgramError::InvalidAccountData);
    }

    validate_code_length(&args.code)?;
    let code = validate_account_code(&args.code)
        .map_err(|_| crate::error::GeolocationError::InvalidAccountCode)?;
    validate_public_ip(&args.public_ip)?;

    let (expected_pda, bump_seed) = get_geo_probe_pda(program_id, &code);
    if probe_account.key != &expected_pda {
        msg!("Invalid GeoProbe PubKey");
        return Err(ProgramError::InvalidSeeds);
    }

    if !probe_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let probe = GeoProbe {
        account_type: AccountType::GeoProbe,
        owner: *payer_account.key,
        exchange_pk: *exchange_account.key,
        public_ip: args.public_ip,
        location_offset_port: args.location_offset_port,
        metrics_publisher_pk: args.metrics_publisher_pk,
        reference_count: 0,
        code,
        parent_devices: vec![],
    };

    try_acc_create(
        &probe,
        probe_account,
        payer_account,
        system_program,
        program_id,
        &[SEED_PREFIX, SEED_PROBE, probe.code.as_bytes(), &[bump_seed]],
    )?;

    Ok(())
}
