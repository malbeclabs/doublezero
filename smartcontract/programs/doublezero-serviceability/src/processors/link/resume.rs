use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{accounttype::AccountType, contributor::Contributor, link::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct LinkResumeArgs {}

impl fmt::Debug for LinkResumeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_resume_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &LinkResumeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_resume_link({:?})", _value);

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );

    let globalstate = globalstate_get(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let contributor = Contributor::try_from(contributor_account)?;
    assert_eq!(contributor.account_type, AccountType::Contributor);
    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }

    let mut link: Link = Link::try_from(link_account)?;
    assert_eq!(link.account_type, AccountType::Link, "Invalid Account Type");

    if link.status != LinkStatus::Suspended {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    link.status = LinkStatus::Activated;

    account_write(link_account, &link, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Resumed: {:?}", link);

    Ok(())
}
