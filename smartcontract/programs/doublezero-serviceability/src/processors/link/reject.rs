use crate::{error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::link::*};
use std::fmt;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct LinkRejectArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub reason: String,
}

impl fmt::Debug for LinkRejectArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "reason: {}", self.reason)
    }
}

pub fn process_reject_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkRejectArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_link({:?})", value);

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    assert!(link_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut link: Link = Link::try_from(link_account)?;
    assert_eq!(link.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        link.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );
    if link.status != LinkStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    link.tunnel_id = 0;
    link.tunnel_net = ([0, 0, 0, 0], 0);
    link.status = LinkStatus::Rejected;
    msg!("Reason: {:?}", value.reason);

    account_write(link_account, &link, payer_account, system_program);

    #[cfg(test)]
    msg!("Rejectd: {:?}", link);

    Ok(())
}
