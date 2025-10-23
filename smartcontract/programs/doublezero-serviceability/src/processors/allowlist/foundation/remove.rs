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
pub struct RemoveFoundationAllowlistArgs {
    pub pubkey: Pubkey,
}

impl fmt::Debug for RemoveFoundationAllowlistArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "pubkey: {}", self.pubkey)
    }
}

pub fn process_remove_foundation_allowlist_globalconfig(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &RemoveFoundationAllowlistArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!(
        "process_remove_foundation_allowlist_globalconfig({:?})",
        value
    );

    // Check the owner of the accounts
    assert_eq!(pda_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(pda_account.is_writable, "PDA Account is not writable");

    let (expected_pda_account, bump_seed) = get_globalstate_pda(program_id);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid GlobalState PubKey"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let mut globalstate = globalstate_get(pda_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    assert_ne!(
        value.pubkey, *payer_account.key,
        "Unable to remove the payer from the foundation allowlist"
    );
    assert!(
        globalstate.foundation_allowlist.len() > 1,
        "Unable to remove the last pubkey from the foundation allowlist"
    );

    globalstate
        .foundation_allowlist
        .retain(|x| x != &value.pubkey);

    globalstate_write_with_realloc(
        pda_account,
        &globalstate,
        payer_account,
        system_program,
        bump_seed,
    );
    #[cfg(test)]
    msg!("Updated: {:?}", globalstate);

    Ok(())
}
