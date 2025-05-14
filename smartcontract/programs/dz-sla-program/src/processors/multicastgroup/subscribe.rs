use crate::error::DoubleZeroError;
use crate::globalstate::globalstate_get;
use crate::helper::*;
use crate::state::multicastgroup::*;
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct MulticastGroupSubscribeArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub subscribers: Vec<Pubkey>,
    pub publishers: Vec<Pubkey>,
}

impl fmt::Debug for MulticastGroupSubscribeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "subscribers: {:?}, publishers: {:?}",
            self.subscribers, self.publishers
        )
    }
}

pub fn process_subscribe_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupSubscribeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_subscribe_multicastgroup({:?})", value);

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

    // Parse the multicastgroup account
    let mut multicastgroup: MulticastGroup =
        MulticastGroup::from(&pda_account.try_borrow_data().unwrap()[..]);
    assert_eq!(
        multicastgroup.index, value.index,
        "Invalid PDA Account Index"
    );
    assert_eq!(
        multicastgroup.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );
    if multicastgroup.owner != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    for subscriber in &value.subscribers {
        if !multicastgroup.subscribers.contains(subscriber) {
            multicastgroup.subscribers.push(*subscriber);
        }
    }
    for publisher in &value.publishers {
        if !multicastgroup.publishers.contains(publisher) {
            multicastgroup.publishers.push(*publisher);
        }
    }

    account_write(pda_account, &multicastgroup, payer_account, system_program);

    #[cfg(test)]
    msg!("Updated: {:?}", multicastgroup);

    Ok(())
}
