use crate::{
    pda::get_geolocation_user_pda,
    seeds::{SEED_GEOUSER, SEED_PREFIX},
    serializer::try_acc_create,
    state::{
        accounttype::AccountType,
        geolocation_user::{
            GeolocationBillingConfig, GeolocationPaymentStatus, GeolocationUser,
            GeolocationUserStatus,
        },
    },
    validation::validate_code_length,
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

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct CreateGeolocationUserArgs {
    pub code: String,
    pub token_account: Pubkey,
}

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
        update_count: 0,
        code,
        token_account: args.token_account,
        payment_status: GeolocationPaymentStatus::Delinquent,
        billing: GeolocationBillingConfig::default(),
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
            SEED_GEOUSER,
            user.code.as_bytes(),
            &[bump_seed],
        ],
    )?;

    Ok(())
}
