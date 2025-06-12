use core::fmt;

use crate::{
    error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::link::*, types::*,
};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct LinkActivateArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
}

impl fmt::Debug for LinkActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "tunnel_id: {}, tunnel_net: {}",
            self.tunnel_id,
            networkv4_to_string(&self.tunnel_net)
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

    link.tunnel_id = value.tunnel_id;
    link.tunnel_net = value.tunnel_net;
    link.status = LinkStatus::Activated;

    account_write(link_account, &link, payer_account, system_program);

    #[cfg(test)]
    msg!("Activated: {:?}", link);

    Ok(())
}
