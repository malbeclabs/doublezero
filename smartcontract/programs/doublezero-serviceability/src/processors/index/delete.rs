use crate::{
    error::DoubleZeroError,
    processors::validation::validate_program_account,
    serializer::try_acc_close,
    state::{globalstate::GlobalState, index::Index},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;

#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct IndexDeleteArgs {}

impl fmt::Debug for IndexDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "IndexDeleteArgs")
    }
}

pub fn process_delete_index(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &IndexDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let index_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_index");

    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(index_account, program_id, writable = true, "Index");
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        "GlobalState"
    );

    // Check foundation allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Verify it's actually an Index account
    let _index = Index::try_from(index_account)?;

    try_acc_close(index_account, payer_account)?;

    #[cfg(test)]
    msg!("Deleted Index account");

    Ok(())
}
