use crate::{error::DoubleZeroError, helper::*, state::link::*};
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
pub struct LinkResumeArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for LinkResumeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_resume_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkResumeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_resume_link({:?})", value);

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );

    let mut link: Link = Link::try_from(link_account)?;
    assert_eq!(link.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        link.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );

    if link.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    if link.status != LinkStatus::Suspended {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    link.status = LinkStatus::Activated;

    account_write(link_account, &link, payer_account, system_program);

    #[cfg(test)]
    msg!("Resumed: {:?}", link);

    Ok(())
}
