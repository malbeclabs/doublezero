use crate::{error::DoubleZeroError, serializer::try_acc_close, state::globalstate::GlobalState};
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
pub struct ResourceExtensionCloseAccountArgs {}

impl fmt::Debug for ResourceExtensionCloseAccountArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_closeaccount_resource_extension(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &ResourceExtensionCloseAccountArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let resource_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_closeaccount_resource_extension({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        resource_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );

    assert!(resource_account.is_writable, "PDA Account is not writable");
    assert!(owner_account.is_writable, "Owner Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    try_acc_close(resource_account, owner_account)?;

    #[cfg(test)]
    msg!("CloseAccount: ResourceExtension closed");

    Ok(())
}
