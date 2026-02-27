use crate::{
    error::GeolocationError,
    pda::get_program_config_pda,
    seeds::{SEED_PREFIX, SEED_PROGRAM_CONFIG},
    serializer::try_acc_create,
    state::{accounttype::AccountType, program_config::GeolocationProgramConfig},
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

use super::parse_upgrade_authority;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct InitProgramConfigArgs {}

pub fn process_init_program_config(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _args: &InitProgramConfigArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let program_config_account = next_account_info(accounts_iter)?;
    let program_data_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    if !payer_account.is_signer {
        msg!("Payer must be a signer");
        return Err(ProgramError::MissingRequiredSignature);
    }
    if !program_config_account.is_writable {
        msg!("ProgramConfig must be writable");
        return Err(ProgramError::InvalidAccountData);
    }

    // Verify the program data account derives from the program id
    let expected_program_data = Pubkey::find_program_address(
        &[program_id.as_ref()],
        &solana_program::bpf_loader_upgradeable::id(),
    )
    .0;
    if program_data_account.key != &expected_program_data {
        msg!("Invalid program data account");
        return Err(ProgramError::InvalidAccountData);
    }

    // Verify payer is the program's upgrade authority
    let program_data = program_data_account.try_borrow_data()?;
    match parse_upgrade_authority(&program_data)? {
        Some(authority) if authority == *payer_account.key => {}
        Some(authority) => {
            msg!(
                "Payer {} is not the upgrade authority {}",
                payer_account.key,
                authority
            );
            return Err(GeolocationError::UnauthorizedInitializer.into());
        }
        None => {
            msg!("Program has no upgrade authority (immutable)");
            return Err(ProgramError::InvalidAccountData);
        }
    }

    let (expected_pda, bump_seed) = get_program_config_pda(program_id);
    if program_config_account.key != &expected_pda {
        msg!("Invalid ProgramConfig Pubkey");
        return Err(ProgramError::InvalidSeeds);
    }

    if !program_config_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let program_config = GeolocationProgramConfig {
        account_type: AccountType::ProgramConfig,
        bump_seed,
        version: 1,
        min_compatible_version: 1,
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
