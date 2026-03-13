use crate::{
    error::DoubleZeroError,
    pda::get_index_pda,
    seeds::{SEED_INDEX, SEED_PREFIX},
    serializer::try_acc_create,
    state::{accounttype::AccountType, globalstate::GlobalState, index::Index},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::validate_account_code;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::fmt;

#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct IndexCreateArgs {
    pub entity_seed: String,
    pub code: String,
}

impl fmt::Debug for IndexCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "entity_seed: {}, code: {}", self.entity_seed, self.code)
    }
}

pub fn process_create_index(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &IndexCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let index_account = next_account_info(accounts_iter)?;
    let entity_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_index({:?})", value);

    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        entity_account.owner, program_id,
        "Invalid Entity Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    assert!(index_account.is_writable, "Index Account is not writable");

    // Check foundation allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Validate and normalize code
    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
    let lowercase_code = code.to_ascii_lowercase();

    // Derive and verify the Index PDA
    let (expected_pda, bump_seed) = get_index_pda(program_id, value.entity_seed.as_bytes(), &code);
    assert_eq!(index_account.key, &expected_pda, "Invalid Index Pubkey");

    // Uniqueness: account must not already exist
    if !index_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    // Verify the entity account is a valid program account
    assert!(!entity_account.data_is_empty(), "Entity Account is empty");

    let index = Index {
        account_type: AccountType::Index,
        pk: *entity_account.key,
        bump_seed,
    };

    try_acc_create(
        &index,
        index_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_INDEX,
            value.entity_seed.as_bytes(),
            lowercase_code.as_bytes(),
            &[bump_seed],
        ],
    )?;

    Ok(())
}
