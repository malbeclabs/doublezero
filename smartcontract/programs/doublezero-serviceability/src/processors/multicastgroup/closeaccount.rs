use crate::{
    error::DoubleZeroError,
    serializer::try_acc_close,
    state::{globalstate::GlobalState, multicastgroup::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct MulticastGroupDeactivateArgs {}

impl fmt::Debug for MulticastGroupDeactivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_closeaccount_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &MulticastGroupDeactivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let multicastgroup_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_deactivate_multicastgroup({:?})", _value);

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
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(
        multicastgroup_account.is_writable,
        "PDA Account is not writable"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let multicastgroup = MulticastGroup::try_from(multicastgroup_account)?;

    if multicastgroup.owner != *owner_account.key {
        return Err(ProgramError::InvalidAccountData);
    }
    if multicastgroup.status != MulticastGroupStatus::Deleting {
        #[cfg(test)]
        msg!("{:?}", multicastgroup);
        return Err(solana_program::program_error::ProgramError::Custom(1));
    }
    if multicastgroup.publisher_count != 0 || multicastgroup.subscriber_count != 0 {
        #[cfg(test)]
        msg!(
            "MulticastGroup has active publishers or subscribers: {:?}",
            multicastgroup
        );
        return Err(solana_program::program_error::ProgramError::Custom(2));
    }

    try_acc_close(multicastgroup_account, owner_account)?;

    #[cfg(test)]
    msg!("Deactivated: MulticastGroup closed");

    Ok(())
}
