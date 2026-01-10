use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{contributor::Contributor, globalstate::GlobalState, link::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkSuspendArgs {}

impl fmt::Debug for LinkSuspendArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_suspend_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &LinkSuspendArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_suspend_link({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(link_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    let contributor = Contributor::try_from(contributor_account)?;

    let payer_in_foundation = globalstate.foundation_allowlist.contains(payer_account.key);

    if contributor.owner != *payer_account.key && !payer_in_foundation {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }

    let mut link: Link = Link::try_from(link_account)?;
    if !payer_in_foundation && link.contributor_pk != *contributor_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if link.status != LinkStatus::Activated {
        #[cfg(test)]
        msg!("{:?}", link);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    link.status = LinkStatus::Suspended;

    try_acc_write(&link, link_account, payer_account, accounts)?;

    msg!("Suspended: {:?}", link);

    Ok(())
}
