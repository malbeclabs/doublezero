use crate::{error::DoubleZeroError, helper::*, state::link::*};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct LinkSuspendArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for LinkSuspendArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_suspend_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkSuspendArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_suspend_link({:?})", value);

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(link_account.is_writable, "PDA Account is not writable");

    let mut link: Link = Link::try_from(link_account)?;
    assert_eq!(link.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        link.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );

    if link.owner != *payer_account.key {
        return Err(ProgramError::InvalidAccountOwner);
    }
    if link.status != LinkStatus::Activated {
        #[cfg(test)]
        msg!("{:?}", link);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    link.status = LinkStatus::Suspended;

    account_write(link_account, &link, payer_account, system_program);

    msg!("Suspended: {:?}", link);

    Ok(())
}
