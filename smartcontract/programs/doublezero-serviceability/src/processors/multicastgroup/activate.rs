use crate::{
    error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::multicastgroup::*,
    types::IpV4,
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;

#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct MulticastGroupActivateArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub multicast_ip: IpV4,
}

impl fmt::Debug for MulticastGroupActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "",)
    }
}

pub fn process_activate_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupActivateArgs,
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
    // Check if the account is writable
    assert!(pda_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut multicastgroup = {
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

    multicastgroup.multicast_ip = value.multicast_ip;
    multicastgroup.status = MulticastGroupStatus::Activated;

    account_write(pda_account, &multicastgroup, payer_account, system_program);

    #[cfg(test)]
    msg!("Activated: {:?}", multicastgroup);

    Ok(())
}
