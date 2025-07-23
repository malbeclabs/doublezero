use core::fmt;

use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get, globalstate_write},
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
pub struct SetInternetLatencyCollectorArgs {
    pub pubkey: Pubkey,
}

impl fmt::Debug for SetInternetLatencyCollectorArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "pubkey: {}", self.pubkey)
    }
}

pub fn process_set_internet_latency_collector_globalstate(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &SetInternetLatencyCollectorArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!(
        "process_set_internet_latency_collector_globalstate({:?})",
        args
    );

    // Check the owning program of the accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let mut globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Check the incoming pubkey is not already the latency collector
    if globalstate.internet_latency_collector == args.pubkey {
        return Err(ProgramError::InvalidArgument);
    }
    globalstate.internet_latency_collector = args.pubkey;

    globalstate_write(globalstate_account, &globalstate)?;
    #[cfg(test)]
    msg!("Updated: {:?}", globalstate);

    Ok(())
}
