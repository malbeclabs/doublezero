use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{globalstate::GlobalState, link::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::types::NetworkV4;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkRejectArgs {
    pub reason: String,
}

impl fmt::Debug for LinkRejectArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "reason: {}", self.reason)
    }
}

pub fn process_reject_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkRejectArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_link({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    assert!(link_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut link: Link = Link::try_from(link_account)?;

    if link.status != LinkStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    link.tunnel_id = 0;
    link.tunnel_net = NetworkV4::default();
    link.status = LinkStatus::Rejected;
    msg!("Reason: {:?}", value.reason);

    try_acc_write(&link, link_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Rejected: {:?}", link);

    Ok(())
}
