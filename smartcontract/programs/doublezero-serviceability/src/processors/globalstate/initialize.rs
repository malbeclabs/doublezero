use crate::{
    accounts::write_account,
    pda::*,
    programversion::ProgramVersion,
    seeds::{SEED_GLOBALSTATE, SEED_PREFIX},
    state::{accounttype::AccountType, globalstate::GlobalState, programconfig::ProgramConfig},
};
use borsh::BorshSerialize;
use doublezero_program_common::create_account::try_create_account;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

pub fn initialize_global_state(program_id: &Pubkey, accounts: &[AccountInfo]) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let program_config_account = next_account_info(accounts_iter)?;
    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("initialize_global_state()");

    let (program_config_pda, program_config_bump_seed) = get_program_config_pda(program_id);
    assert_eq!(
        program_config_account.key, &program_config_pda,
        "Invalid ProgramConfig PubKey"
    );
    write_account(
        program_config_account,
        &ProgramConfig {
            account_type: AccountType::ProgramConfig,
            bump_seed: program_config_bump_seed, // This is not used in this context
            version: ProgramVersion::current(),  // Default version for initialization
        },
        program_id,
        payer_account,
        system_program,
    )?;

    let (expected_pda_account, bump_seed) = get_globalstate_pda(program_id);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid GlobalState PubKey"
    );

    // Check if the account is already initialized
    if !pda_account.data.borrow().is_empty() {
        return Ok(());
    }

    // Create the GlobalState account
    let data = GlobalState {
        account_type: AccountType::GlobalState,
        bump_seed,
        account_index: 0,
        foundation_allowlist: vec![*payer_account.key],
        device_allowlist: vec![*payer_account.key],
        user_allowlist: vec![],
        activator_authority_pk: *payer_account.key,
        sentinel_authority_pk: *payer_account.key,
        contributor_airdrop_lamports: 1_000_000_000,
        user_airdrop_lamports: 40_000,
    };

    // Size of our index account
    let account_space = data.size();

    if pda_account.try_borrow_data()?.is_empty() {
        // Create the index account
        try_create_account(
            payer_account.key,      // Account paying for the new account
            pda_account.key,        // Account to be created
            pda_account.lamports(), // Current amount of lamports on the new account
            account_space,          // Size in bytes to allocate for the data field
            program_id,             // Set program owner to our program
            accounts,
            &[SEED_PREFIX, SEED_GLOBALSTATE, &[bump_seed]],
        )?;
    }

    let mut account_data = &mut pda_account.data.borrow_mut()[..];
    data.serialize(&mut account_data).unwrap();

    #[cfg(test)]
    msg!("{:?}", account_data);

    Ok(())
}
