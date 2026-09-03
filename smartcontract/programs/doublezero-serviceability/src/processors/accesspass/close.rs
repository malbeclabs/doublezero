use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    serializer::try_acc_close,
    state::{
        accesspass::{AccessPass, AccessPassKind},
        accounttype::AccountType,
        globalstate::GlobalState,
        permission::permission_flags,
    },
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
    expected: Option<AccessPassKind>,
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

    // Parse the global state account & check authorization
    let globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::ACCESS_PASS_ADMIN,
    )?;

    // These checks used to sit inside `if let Ok(data) = accesspass_account.try_borrow_data()`,
    // with an `else` that logged a warning and fell through to the close. A failed borrow
    // therefore closed the pass with neither the account-type check nor the connection check
    // applied. Read the account once here and let a failed read stop the instruction.
    let account_type: AccountType = accesspass_account.try_borrow_data()?[0].into();
    if account_type != AccountType::AccessPass {
        msg!("AccountType is not AccessPass, cannot close");
        return Err(DoubleZeroError::InvalidAccountType.into());
    }
    let accesspass = AccessPass::try_from(accesspass_account)?;

    // `None` is the deprecated `CloseAccessPass` (variant 69), which predates the
    // per-pass-type split and performs no kind check. It is removed, along with this
    // `Option`, in the follow-up that moves every caller. See malbeclabs/infra#2470.
    if let Some(expected) = expected {
        let actual = AccessPassKind::from(&accesspass.accesspass_type);
        if actual != expected {
            msg!("this instruction closes a {expected} pass, but the pass is {actual}");
            return Err(DoubleZeroError::InvalidAccessPassType.into());
        }
    }

    // Feed authority can only close access passes they own
    if globalstate.feed_authority_pk == *payer_account.key && accesspass.owner != *payer_account.key
    {
        msg!("Feed authority can only close access passes they own");
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if accesspass.connection_count != 0 {
        msg!(
            "AccessPass has {} active connections, cannot close",
            accesspass.connection_count
        );
        return Err(DoubleZeroError::AccessPassInUse.into());
    }

    msg!("AccountType is AccessPass and there are no active connections, proceeding to close");

    try_acc_close(accesspass_account, payer_account)?;

    msg!("Access pass closed");

    Ok(())
}
