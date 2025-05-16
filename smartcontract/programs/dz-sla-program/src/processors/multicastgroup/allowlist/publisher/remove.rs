use crate::error::DoubleZeroError;
use crate::helper::account_write;
use crate::state::multicastgroup::MulticastGroup;
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
pub struct RemoveMulticastGroupPubAllowlistArgs {
    pub pubkey: Pubkey,
}

impl fmt::Debug for RemoveMulticastGroupPubAllowlistArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "pubkey: {}", self.pubkey)
    }
}

pub fn process_remove_multicast_pub_allowlist(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &RemoveMulticastGroupPubAllowlistArgs,
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

    mgroup.pub_allowlist.retain(|x| x != &value.pubkey);

    account_write(mgroup_account, &mgroup, payer_account, system_program);

    #[cfg(test)]
    msg!("Updated: {:?}", mgroup);

    Ok(())
}
