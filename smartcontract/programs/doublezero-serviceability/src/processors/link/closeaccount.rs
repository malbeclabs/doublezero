use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{accounttype::AccountType, contributor::Contributor, device::Device, link::*},
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
pub struct LinkCloseAccountArgs {}

impl fmt::Debug for LinkCloseAccountArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_closeaccount_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &LinkCloseAccountArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let side_a_account = next_account_info(accounts_iter)?;
    let side_z_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_closeaccount_link({:?})", _value);

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
    );
    assert_eq!(
        side_a_account.owner, program_id,
        "Invalid Side A Account Owner"
    );
    assert_eq!(
        side_z_account.owner, program_id,
        "Invalid Side Z Account Owner"
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
    assert!(link_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut contributor = Contributor::try_from(contributor_account)?;
    assert_eq!(contributor.account_type, AccountType::Contributor);
    let mut side_a_dev = Device::try_from(side_a_account)?;
    assert_eq!(side_a_dev.account_type, AccountType::Device);
    let mut side_z_dev = Device::try_from(side_z_account)?;
    assert_eq!(side_z_dev.account_type, AccountType::Device);

    let link: Link = Link::try_from(link_account)?;
    assert_eq!(link.account_type, AccountType::Link, "Invalid Account Type");

    if link.owner != *owner_account.key {
        return Err(ProgramError::InvalidAccountData);
    }
    if link.status != LinkStatus::Deleting {
        #[cfg(test)]
        msg!("{:?}", link);
        return Err(solana_program::program_error::ProgramError::Custom(1));
    }

    contributor.reference_count = contributor.reference_count.saturating_sub(1);
    side_a_dev.reference_count = side_a_dev.reference_count.saturating_sub(1);
    side_z_dev.reference_count = side_z_dev.reference_count.saturating_sub(1);

    account_write(
        contributor_account,
        &contributor,
        payer_account,
        system_program,
    )?;
    account_write(side_a_account, &side_a_dev, payer_account, system_program)?;
    account_write(side_z_account, &side_z_dev, payer_account, system_program)?;
    account_close(link_account, owner_account)?;

    #[cfg(test)]
    msg!("CloseAccount: Link closed");

    Ok(())
}
