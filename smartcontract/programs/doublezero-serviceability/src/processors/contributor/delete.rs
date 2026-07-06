use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    serializer::try_acc_close,
    state::{contributor::*, globalstate::GlobalState, permission::permission_flags},
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
pub struct ContributorDeleteArgs {}

impl fmt::Debug for ContributorDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_delete_contributor(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &ContributorDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_contributor({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    assert!(
        contributor_account.is_writable,
        "PDA Account is not writable"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    // Authorization: CONTRIBUTOR_ADMIN (Permission account) or foundation (legacy).
    let globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::CONTRIBUTOR_ADMIN,
    )?;

    let contributor = Contributor::try_from(contributor_account)?;
    if matches!(contributor.status, ContributorStatus::Deleting) {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    if contributor.reference_count > 0 {
        return Err(DoubleZeroError::ReferenceCountNotZero.into());
    }

    try_acc_close(contributor_account, payer_account)?;

    #[cfg(test)]
    msg!("Deleted: {:?}", contributor_account);

    Ok(())
}
