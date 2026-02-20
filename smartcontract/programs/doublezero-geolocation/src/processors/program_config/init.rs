use crate::{
    error::GeolocationError,
    instructions::InitProgramConfigArgs,
    pda::get_program_config_pda,
    seeds::{SEED_PREFIX, SEED_PROGRAM_CONFIG},
    serializer::try_acc_create,
    state::{accounttype::AccountType, program_config::GeolocationProgramConfig},
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

/// Parse the upgrade authority from a BPF Upgradeable Loader ProgramData account.
///
/// ProgramData layout (bincode-serialized UpgradeableLoaderState):
///   [0..4]   u32 LE  variant discriminant  (3 = ProgramData)
///   [4..12]  i64 LE  slot
///   [12]     u8      Option tag  (0 = None, 1 = Some)
///   [13..45] [u8;32] upgrade authority pubkey (present only when tag == 1)
fn parse_upgrade_authority(data: &[u8]) -> Result<Option<Pubkey>, ProgramError> {
    const PROGRAM_DATA_DISCRIMINANT: u32 = 3;
    const MIN_LEN: usize = 4 + 8 + 1; // discriminant + slot + option tag

    if data.len() < MIN_LEN {
        return Err(ProgramError::InvalidAccountData);
    }

    let discriminant = u32::from_le_bytes(
        data[0..4]
            .try_into()
            .map_err(|_| ProgramError::InvalidAccountData)?,
    );
    if discriminant != PROGRAM_DATA_DISCRIMINANT {
        return Err(ProgramError::InvalidAccountData);
    }

    match data[12] {
        0 => Ok(None),
        1 => {
            if data.len() < MIN_LEN + 32 {
                return Err(ProgramError::InvalidAccountData);
            }
            let authority =
                Pubkey::try_from(&data[13..45]).map_err(|_| ProgramError::InvalidAccountData)?;
            Ok(Some(authority))
        }
        _ => Err(ProgramError::InvalidAccountData),
    }
}

pub fn process_init_program_config(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &InitProgramConfigArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let program_config_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;
    let program_data_account = next_account_info(accounts_iter)?;

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

    let (expected_pda, bump_seed) = get_program_config_pda(program_id);
    if program_config_account.key != &expected_pda {
        msg!("Invalid ProgramConfig PubKey");
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
