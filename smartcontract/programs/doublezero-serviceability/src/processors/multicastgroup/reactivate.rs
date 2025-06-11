use core::fmt;

use crate::{
    error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::multicastgroup::*,
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
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct MulticastGroupReactivateArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for MulticastGroupReactivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_reactivate_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupReactivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_reactivate_multicastgroup({:?})", value);

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

    // Parse the global state account & check if the payer is in the allowlist
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
    multicastgroup.status = MulticastGroupStatus::Activated;

    account_write(pda_account, &multicastgroup, payer_account, system_program);

    #[cfg(test)]
    msg!("Suspended: {:?}", multicastgroup);

    Ok(())
}
