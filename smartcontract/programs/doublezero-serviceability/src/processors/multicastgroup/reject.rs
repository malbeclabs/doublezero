use crate::{
    error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::multicastgroup::*,
};
use std::fmt;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct MulticastGroupRejectArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub reason: String,
}

impl fmt::Debug for MulticastGroupRejectArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "reason: {}", self.reason)
    }
}

pub fn process_reject_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupRejectArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_multicastgroup({:?})", value);

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
    assert!(pda_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut multicastgroup: MulticastGroup = {
        let account_data = pda_account
            .try_borrow_data()
            .map_err(|_| ProgramError::AccountBorrowFailed)?;
        MulticastGroup::from(&account_data[..])
    };
    assert_eq!(
        multicastgroup.index, value.index,
        "Invalid PDA Account Index"
    );
    assert_eq!(
        multicastgroup.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );
    if multicastgroup.status != MulticastGroupStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    multicastgroup.status = MulticastGroupStatus::Rejected;
    msg!("Reason: {:?}", value.reason);

    account_write(pda_account, &multicastgroup, payer_account, system_program);

    #[cfg(test)]
    msg!("Rejectd: {:?}", multicastgroup);

    Ok(())
}
