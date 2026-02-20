use crate::{
    instructions::CreateGeolocationUserArgs,
    pda::get_geolocation_user_pda,
    seeds::{SEED_GEO_USER, SEED_PREFIX},
    serializer::try_acc_create,
    state::{
        accounttype::AccountType,
        geolocation_user::{
            FlatPerEpochConfig, GeolocationBillingConfig, GeolocationPaymentStatus,
            GeolocationUser, GeolocationUserStatus,
        },
    },
    validation::validate_code_length,
};
use doublezero_program_common::validate_account_code;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

pub fn process_create_geolocation_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &CreateGeolocationUserArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
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

    validate_code_length(&args.code)?;
    let code = validate_account_code(&args.code)
        .map_err(|_| crate::error::GeolocationError::InvalidAccountCode)?;

    let (expected_pda, bump_seed) = get_geolocation_user_pda(program_id, &code);
    if user_account.key != &expected_pda {
        msg!("Invalid GeolocationUser PubKey");
        return Err(ProgramError::InvalidSeeds);
    }

    if !user_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let user = GeolocationUser {
        account_type: AccountType::GeolocationUser,
        owner: *payer_account.key,
        bump_seed,
        code,
        token_account: args.token_account,
        payment_status: GeolocationPaymentStatus::Delinquent,
        billing: GeolocationBillingConfig::FlatPerEpoch(FlatPerEpochConfig::default()),
        status: GeolocationUserStatus::Activated,
        targets: vec![],
    };

    try_acc_create(
        &user,
        user_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_GEO_USER,
            user.code.as_bytes(),
            &[bump_seed],
        ],
    )?;

    Ok(())
}
