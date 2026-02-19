use crate::{
    error::DoubleZeroError,
    pda::*,
    seeds::{SEED_CONTRIBUTOR, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{accounttype::AccountType, contributor::*, globalstate::GlobalState},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::validate_account_code;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program::invoke_signed_unchecked,
    program_error::ProgramError,
    pubkey::Pubkey,
    rent::Rent,
    sysvar::Sysvar,
};

use std::fmt;

#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct ContributorCreateArgs {
    pub code: String,
}

impl fmt::Debug for ContributorCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "code: {}", self.code)
    }
}

pub fn process_create_contributor(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ContributorCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let contributor_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_contributor({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate and normalize code
    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

    // Check the owner of the accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(
        contributor_account.is_writable,
        "PDA Account is not writable"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let mut globalstate = GlobalState::try_from(globalstate_account)?;
    globalstate.account_index += 1;

    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }
    // get the PDA pubkey and bump seed for the account contributor & check if it matches the account
    let (expected_pda_account, bump_seed) =
        get_contributor_pda(program_id, globalstate.account_index);
    assert_eq!(
        contributor_account.key, &expected_pda_account,
        "Invalid Contributor PubKey"
    );

    // Check if the account is already initialized
    if !contributor_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let contributor = Contributor {
        account_type: AccountType::Contributor,
        owner: *owner_account.key,
        index: globalstate.account_index,
        reference_count: 0,
        bump_seed,
        code,
        status: ContributorStatus::Activated,
        ops_manager_pk: Pubkey::default(),
    };

    let deposit = Rent::get()
        .unwrap()
        .minimum_balance(0)
        .saturating_add(globalstate.contributor_airdrop_lamports);

    invoke_signed_unchecked(
        &solana_system_interface::instruction::transfer(
            payer_account.key,
            owner_account.key,
            deposit,
        ),
        &[
            payer_account.clone(),
            owner_account.clone(),
            system_program.clone(),
        ],
        &[],
    )?;

    try_acc_create(
        &contributor,
        contributor_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_CONTRIBUTOR,
            &globalstate.account_index.to_le_bytes(),
            &[bump_seed],
        ],
    )?;
    try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;

    Ok(())
}
