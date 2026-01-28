use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{
        accounttype::AccountType, contributor::Contributor, globalstate::GlobalState, link::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkDeleteArgs {}

impl fmt::Debug for LinkDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_delete_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &LinkDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_link({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
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

    let globalstate = GlobalState::try_from(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let contributor = Contributor::try_from(contributor_account)?;

    let payer_in_foundation = globalstate.foundation_allowlist.contains(payer_account.key);

    if contributor.owner != *payer_account.key && !payer_in_foundation {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }

    // Any link can be deleted by its contributor or foundation allowlist when Activated or Suspended
    let mut link: Link = Link::try_from(link_account)?;
    if link.status != LinkStatus::Activated {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    link.status = LinkStatus::Deleting;

    try_acc_write(&link, link_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Deleting: {:?}", link);

    Ok(())
}
