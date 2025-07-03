use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{accounttype::AccountType, multicastgroup::*},
};
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
pub struct MulticastGroupUpdateArgs {
    pub code: Option<String>,
    pub multicast_ip: Option<std::net::Ipv4Addr>,
    pub max_bandwidth: Option<u64>,
}

impl fmt::Debug for MulticastGroupUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {:?}, multicast_ip: {:?}",
            self.code, self.multicast_ip
        )
    }
}

pub fn process_update_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let multicastgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_multicastgroup({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        multicastgroup_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    assert!(
        multicastgroup_account.is_writable,
        "PDA Account is not writable"
    );
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Parse the multicastgroup account
    let mut multicastgroup: MulticastGroup = MulticastGroup::try_from(multicastgroup_account)?;
    assert_eq!(
        multicastgroup.account_type,
        AccountType::MulticastGroup,
        "Invalid Account Type"
    );

    if let Some(ref code) = value.code {
        multicastgroup.code = code.clone();
    }
    if let Some(ref multicast_ip) = value.multicast_ip {
        multicastgroup.multicast_ip = *multicast_ip;
    }
    if let Some(ref max_bandwidth) = value.max_bandwidth {
        multicastgroup.max_bandwidth = *max_bandwidth;
    }

    account_write(
        multicastgroup_account,
        &multicastgroup,
        payer_account,
        system_program,
    );

    #[cfg(test)]
    msg!("Updated: {:?}", multicastgroup);

    Ok(())
}
