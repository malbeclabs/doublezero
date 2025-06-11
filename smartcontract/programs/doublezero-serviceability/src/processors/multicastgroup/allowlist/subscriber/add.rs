use core::fmt;

use crate::{error::DoubleZeroError, helper::account_write, state::multicastgroup::MulticastGroup};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct AddMulticastGroupSubAllowlistArgs {
    pub pubkey: Pubkey,
}

impl fmt::Debug for AddMulticastGroupSubAllowlistArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "pubkey: {}", self.pubkey)
    }
}

pub fn process_add_multicastgroup_sub_allowlist(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &AddMulticastGroupSubAllowlistArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_add_user_allowlist({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        mgroup_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(mgroup_account.is_writable, "PDA Account is not writable");

    // Parse the global state account & check if the payer is in the allowlist
    let mut mgroup = MulticastGroup::from(mgroup_account);
    if mgroup.owner != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if !mgroup.sub_allowlist.contains(&value.pubkey) {
        mgroup.sub_allowlist.push(value.pubkey);
    }

    account_write(mgroup_account, &mgroup, payer_account, system_program);

    #[cfg(test)]
    msg!("Updated: {:?}", mgroup);

    Ok(())
}
