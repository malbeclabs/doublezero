use crate::{
    pda::*,
    programversion::ProgramVersion,
    seeds::{SEED_GLOBALSTATE, SEED_PREFIX, SEED_PROGRAM_CONFIG},
    serializer::{try_acc_create, try_acc_write},
    state::{accounttype::AccountType, globalstate::GlobalState, programconfig::ProgramConfig},
};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::str::FromStr;

pub fn initialize_global_state(program_id: &Pubkey, accounts: &[AccountInfo]) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let program_config_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_account = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("initialize_global_state()");

    assert!(
        program_config_account.is_writable,
        "ProgramConfig must be writable"
    );
    assert!(
        globalstate_account.is_writable,
        "GlobalState must be writable"
    );
    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(
        system_account.key,
        &solana_system_interface::program::ID,
        "Invalid System Program account"
    );

    let (program_config_pda, program_config_bump_seed) = get_program_config_pda(program_id);
    assert_eq!(
        program_config_account.key, &program_config_pda,
        "Invalid ProgramConfig PubKey"
    );

    let program_config = ProgramConfig {
        account_type: AccountType::ProgramConfig,
        bump_seed: program_config_bump_seed, // This is not used in this context
        version: ProgramVersion::current(),  // Default version for initialization
        min_compatible_version: ProgramVersion::from_str(
            crate::min_version::MIN_COMPATIBLE_VERSION,
        )
        .unwrap(),
    };

    // Create the ProgramConfig account if it doesn't exist
    if program_config_account.data_is_empty() {
        try_acc_create(
            &program_config,
            program_config_account,
            payer_account,
            system_account,
            program_id,
            &[
                SEED_PREFIX,
                SEED_PROGRAM_CONFIG,
                &[program_config_bump_seed],
            ],
        )?;
    } else {
        try_acc_write(
            &program_config,
            program_config_account,
            payer_account,
            accounts,
        )?;
    }

    let (expected_globalstate_account, bump_seed) = get_globalstate_pda(program_id);
    assert_eq!(
        globalstate_account.key, &expected_globalstate_account,
        "Invalid GlobalState PubKey"
    );

    // Check if the account is already initialized
    if !globalstate_account.data_is_empty() {
        return Ok(());
    }

    // Create the GlobalState account
    let globalstate = GlobalState {
        account_type: AccountType::GlobalState,
        bump_seed,
        account_index: 0,
        foundation_allowlist: vec![*payer_account.key],
        _device_allowlist: vec![*payer_account.key],
        _user_allowlist: vec![],
        activator_authority_pk: *payer_account.key,
        sentinel_authority_pk: *payer_account.key,
        contributor_airdrop_lamports: 1_000_000_000,
        user_airdrop_lamports: 40_000,
        health_oracle_pk: *payer_account.key,
        qa_allowlist: vec![*payer_account.key],
        feature_flags: 0,
        reservation_authority_pk: Pubkey::default(),
    };

    try_acc_create(
        &globalstate,
        globalstate_account,
        payer_account,
        system_account,
        program_id,
        &[SEED_PREFIX, SEED_GLOBALSTATE, &[bump_seed]],
    )?;

    #[cfg(test)]
    msg!("{:?}", globalstate);

    Ok(())
}
