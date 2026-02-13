use crate::{
    instructions::CreateGeoProbeArgs,
    pda::get_geo_probe_pda,
    processors::check_foundation_allowlist,
    seeds::{SEED_PREFIX, SEED_PROBE},
    serializer::try_acc_create,
    state::{accounttype::AccountType, geo_probe::GeoProbe},
    validation::{validate_code_length, validate_public_ip},
};
use doublezero_program_common::validate_account_code;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

pub fn process_create_geo_probe(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &CreateGeoProbeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let probe_account = next_account_info(accounts_iter)?;
    let program_config_account = next_account_info(accounts_iter)?;
    let serviceability_globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    if !payer_account.is_signer {
        msg!("Payer must be a signer");
        return Err(ProgramError::MissingRequiredSignature);
    }
    if system_program.key != &solana_program::system_program::id() {
        msg!("Invalid System Program account");
        return Err(ProgramError::IncorrectProgramId);
    }

    check_foundation_allowlist(
        program_config_account,
        serviceability_globalstate_account,
        payer_account,
        program_id,
    )?;

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
        bump_seed,
        exchange_pk: args.exchange_pk,
        public_ip: args.public_ip,
        port: args.port,
        code,
        parent_devices: vec![],
        metrics_publisher_pk: args.metrics_publisher_pk,
        latency_threshold_ns: args.latency_threshold_ns,
        reference_count: 0,
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
