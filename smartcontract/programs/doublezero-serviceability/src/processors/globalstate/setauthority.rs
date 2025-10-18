use core::fmt;

use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get, globalstate_write_with_realloc},
    pda::*,
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct SetAuthorityArgs {
    pub activator_authority_pk: Option<Pubkey>,
    pub sentinel_authority_pk: Option<Pubkey>,
}

impl fmt::Debug for SetAuthorityArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "activator_authority_pk: {:?}, sentinel_authority_pk: {:?}",
            self.activator_authority_pk, self.sentinel_authority_pk
        )
    }
}

pub fn process_set_authority(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetAuthorityArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_set_authority({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(
        globalstate_account.is_writable,
        "PDA Account is not writable"
    );

    let (expected_pda_account, bump_seed) = get_globalstate_pda(program_id);
    assert_eq!(
        globalstate_account.key, &expected_pda_account,
        "Invalid GlobalState PubKey"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let mut globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if let Some(activator_authority_pk) = value.activator_authority_pk {
        globalstate.activator_authority_pk = activator_authority_pk;
    }

    if let Some(sentinel_authority_pk) = value.sentinel_authority_pk {
        globalstate.sentinel_authority_pk = sentinel_authority_pk;
    }

    globalstate_write_with_realloc(
        globalstate_account,
        &globalstate,
        payer_account,
        system_program,
        bump_seed,
    );
    #[cfg(test)]
    msg!("Updated: {:?}", globalstate);

    Ok(())
}
