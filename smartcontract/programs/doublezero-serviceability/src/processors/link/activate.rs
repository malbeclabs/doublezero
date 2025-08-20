use crate::{error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::link::*};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct LinkActivateArgs {
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
}

impl fmt::Debug for LinkActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "tunnel_id: {}, tunnel_net: {}",
            self.tunnel_id, &self.tunnel_net,
        )
    }
}

pub fn process_activate_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkActivateArgs,
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
    // Check if the account is writable
    assert!(link_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut link: Link = Link::try_from(link_account)?;

    if link.status != LinkStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    link.tunnel_id = value.tunnel_id;
    link.tunnel_net = value.tunnel_net;
    link.status = LinkStatus::Activated;

    account_write(link_account, &link, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Activated: {:?}", link);

    Ok(())
}
