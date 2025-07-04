use crate::{
    error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::contributor::*,
};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct ContributorUpdateArgs {
    pub code: Option<String>,
    pub ata_owner_pk: Option<Pubkey>,
}

impl fmt::Debug for ContributorUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {:?}, ata_owner_pk: {:?}",
            self.code, self.ata_owner_pk
        )
    }
}

pub fn process_update_contributor(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ContributorUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_contributor({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    assert!(
        contributor_account.is_writable,
        "PDA Account is not writable"
    );
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Parse the contributor account
    let mut contributor: Contributor = Contributor::try_from(contributor_account)?;

    if let Some(ref code) = value.code {
        contributor.code = code.clone();
    }
    if let Some(ref ata_owner_pk) = value.ata_owner_pk {
        contributor.ata_owner_pk = *ata_owner_pk;
    }

    account_write(
        contributor_account,
        &contributor,
        payer_account,
        system_program,
    );

    #[cfg(test)]
    msg!("Updated: {:?}", contributor);

    Ok(())
}
