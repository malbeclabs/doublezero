use crate::{
    error::GeolocationError, pda::get_program_config_pda, serializer::try_acc_write,
    state::program_config::GeolocationProgramConfig,
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
pub struct UpdateProgramConfigArgs {
    pub serviceability_program_id: Option<Pubkey>,
    pub version: Option<u32>,
    pub min_compatible_version: Option<u32>,
}

pub fn process_update_program_config(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &UpdateProgramConfigArgs,
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
    if system_program.key != &solana_program::system_program::id() {
        msg!("Invalid System Program account");
        return Err(ProgramError::IncorrectProgramId);
    }
    if !program_config_account.is_writable {
        msg!("ProgramConfig must be writable");
        return Err(ProgramError::InvalidAccountData);
    }
    if program_config_account.owner != program_id {
        msg!("Invalid ProgramConfig Account Owner");
        return Err(ProgramError::IllegalOwner);
    }

    // Verify the program data account is owned by the BPF Upgradeable Loader
    if program_data_account.owner != &solana_program::bpf_loader_upgradeable::id() {
        msg!("Program data account not owned by BPF Upgradeable Loader");
        return Err(ProgramError::IllegalOwner);
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
    drop(program_data);

    let (expected_pda, _) = get_program_config_pda(program_id);
    if program_config_account.key != &expected_pda {
        msg!("Invalid ProgramConfig Pubkey");
        return Err(ProgramError::InvalidSeeds);
    }

    let mut program_config = GeolocationProgramConfig::try_from(program_config_account)?;

    if let Some(serviceability_program_id) = args.serviceability_program_id {
        program_config.serviceability_program_id = serviceability_program_id;
    }
    if let Some(version) = args.version {
        program_config.version = version;
    }
    if let Some(min_compatible_version) = args.min_compatible_version {
        program_config.min_compatible_version = min_compatible_version;
    }
    if program_config.min_compatible_version > program_config.version {
        return Err(GeolocationError::InvalidMinCompatibleVersion.into());
    }

    try_acc_write(
        &program_config,
        program_config_account,
        payer_account,
        accounts,
    )?;

    Ok(())
}
