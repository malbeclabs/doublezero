use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get_next,
    helper::*,
    state::{accounttype::AccountType, multicastgroup::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct MulticastGroupCloseAccountArgs {}

impl fmt::Debug for MulticastGroupCloseAccountArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_closeaccount_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &MulticastGroupCloseAccountArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let multicastgroup_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_closeaccount_multicastgroup({:?})", _value);

    // Check the owner of the accounts
    assert_eq!(
        multicastgroup_account.owner, program_id,
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
    // Check if the account is writable
    assert!(
        multicastgroup_account.is_writable,
        "PDA Account is not writable"
    );

    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let multicastgroup = MulticastGroup::try_from(multicastgroup_account)?;
    assert_eq!(
        multicastgroup.account_type,
        AccountType::MulticastGroup,
        "Invalid Account Type"
    );

    if multicastgroup.owner != *owner_account.key {
        return Err(ProgramError::InvalidAccountData);
    }
    if multicastgroup.status != MulticastGroupStatus::Deleting {
        #[cfg(test)]
        msg!("{:?}", multicastgroup);
        return Err(solana_program::program_error::ProgramError::Custom(1));
    }

    account_close(multicastgroup_account, owner_account)?;

    #[cfg(test)]
    msg!("Closed account: MulticastGroup closed");

    Ok(())
}
