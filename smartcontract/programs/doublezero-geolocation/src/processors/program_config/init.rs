use crate::{
    instructions::InitProgramConfigArgs,
    pda::get_program_config_pda,
    seeds::{SEED_PREFIX, SEED_PROGRAM_CONFIG},
    serializer::try_acc_create,
    state::{accounttype::AccountType, program_config::GeolocationProgramConfig},
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

pub fn process_init_program_config(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &InitProgramConfigArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let program_config_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(
        system_program.key,
        &solana_program::system_program::id(),
        "Invalid System Program account"
    );
    assert!(
        program_config_account.is_writable,
        "ProgramConfig must be writable"
    );

    let (expected_pda, bump_seed) = get_program_config_pda(program_id);
    assert_eq!(
        program_config_account.key, &expected_pda,
        "Invalid ProgramConfig PubKey"
    );

    if !program_config_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let program_config = GeolocationProgramConfig {
        account_type: AccountType::ProgramConfig,
        bump_seed,
        version: 1,
        min_compatible_version: 1,
        serviceability_program_id: args.serviceability_program_id,
    };

    try_acc_create(
        &program_config,
        program_config_account,
        payer_account,
        system_program,
        program_id,
        &[SEED_PREFIX, SEED_PROGRAM_CONFIG, &[bump_seed]],
    )?;

    Ok(())
}
