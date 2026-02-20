use crate::{
    error::DoubleZeroError,
    serializer::try_acc_close,
    state::{accesspass::AccessPass, accounttype::AccountType, globalstate::GlobalState},
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
pub struct CloseAccessPassArgs {}

impl fmt::Debug for CloseAccessPassArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_close_access_pass(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &CloseAccessPassArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_close_accesspass({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid AccessPass Account Owner"
    );

    // Check the owner of the accounts
    assert_eq!(
        *globalstate_account.owner,
        program_id.clone(),
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(
        accesspass_account.is_writable,
        "PDA Account is not writable"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if let Ok(data) = accesspass_account.try_borrow_data() {
        let account_type: AccountType = data[0].into();
        if account_type != AccountType::AccessPass {
            msg!("AccountType is not AccessPass, cannot close");
            return Err(DoubleZeroError::InvalidAccountType.into());
        }
        let accesspass = AccessPass::try_from(accesspass_account)?;

        if accesspass.connection_count != 0 {
            msg!(
                "AccessPass has {} active connections, cannot close",
                accesspass.connection_count
            );
            return Err(DoubleZeroError::AccessPassInUse.into());
        }

        msg!("AccountType is AccessPass and there are no active connections, proceeding to close");
    } else {
        msg!("Failed to borrow account data, cannot close");
    }

    try_acc_close(accesspass_account, payer_account)?;

    msg!("Access pass closed");

    Ok(())
}
