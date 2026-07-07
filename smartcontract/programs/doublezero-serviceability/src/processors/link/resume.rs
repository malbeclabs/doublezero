use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    processors::validation::validate_program_account,
    serializer::try_acc_write,
    state::{
        contributor::Contributor, globalstate::GlobalState, link::*, permission::permission_flags,
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
pub struct LinkResumeArgs {}

impl fmt::Debug for LinkResumeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_resume_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &LinkResumeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_resume_link({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(link_account, program_id, writable = true, "Link");
    validate_program_account!(contributor_account, program_id, writable = false, "Contributor");
    validate_program_account!(globalstate_account, program_id, writable = false, "GlobalState");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;
    let contributor = Contributor::try_from(contributor_account)?;

    // Authorization: the contributor owner, or NETWORK_ADMIN (Permission account) /
    // foundation (legacy). Privileged callers bypass the per-link contributor binding.
    let is_privileged = authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::NETWORK_ADMIN,
    )
    .is_ok();

    if contributor.owner != *payer_account.key && !is_privileged {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }

    let mut link: Link = Link::try_from(link_account)?;

    if !is_privileged && link.contributor_pk != *contributor_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if link.status != LinkStatus::Suspended {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    link.status = LinkStatus::Activated;

    try_acc_write(&link, link_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Resumed: {:?}", link);

    Ok(())
}
