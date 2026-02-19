use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{globalstate::GlobalState, multicastgroup::*},
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
pub struct MulticastGroupSuspendArgs {}

impl fmt::Debug for MulticastGroupSuspendArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_suspend_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &MulticastGroupSuspendArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let multicastgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_suspend_multicastgroup({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

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
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    assert!(
        multicastgroup_account.is_writable,
        "PDA Account is not writable"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut multicastgroup: MulticastGroup = MulticastGroup::try_from(multicastgroup_account)?;

    if multicastgroup.status != MulticastGroupStatus::Activated {
        #[cfg(test)]
        msg!("{:?}", multicastgroup);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    multicastgroup.status = MulticastGroupStatus::Suspended;

    try_acc_write(
        &multicastgroup,
        multicastgroup_account,
        payer_account,
        accounts,
    )?;

    #[cfg(test)]
    msg!("Suspended: {:?}", multicastgroup);

    Ok(())
}
