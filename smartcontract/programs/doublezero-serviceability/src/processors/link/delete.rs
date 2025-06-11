use crate::{error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::link::*};
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
pub struct LinkDeleteArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for LinkDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_delete_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_link({:?})", value);

    // Check the owner of the accounts
    assert_eq!(pda_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );

    let mut tunnel: Link = Link::from(&pda_account.try_borrow_data().unwrap()[..]);
    assert_eq!(tunnel.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        tunnel.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key)
        && tunnel.owner != *payer_account.key
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    tunnel.status = LinkStatus::Deleting;

    account_write(pda_account, &tunnel, payer_account, system_program);

    #[cfg(test)]
    msg!("Deleting: {:?}", tunnel);

    Ok(())
}
