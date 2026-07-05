use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{accesspass::AccessPass, globalstate::GlobalState, permission::permission_flags},
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
pub struct CheckStatusAccessPassArgs {}

impl fmt::Debug for CheckStatusAccessPassArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_check_status_access_pass(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &CheckStatusAccessPassArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    /*  Accounts prefixed with an underscore are not currently used.
        They are kept for backward compatibility and may be removed in future releases.
    */
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_check_status_access_pass({:?})", _value);

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
    // Check if the account is writable
    assert!(
        accesspass_account.is_writable,
        "PDA Account is not writable"
    );

    // Authorization: ACTIVATOR or foundation, via a Permission account or the legacy
    // activator_authority_pk / foundation_allowlist (ACTIVATOR covers the activator
    // authority, USER_ADMIN covers foundation).
    let globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::ACTIVATOR | permission_flags::USER_ADMIN,
    )?;

    let mut accesspass = AccessPass::try_from(accesspass_account)?;
    // Update status
    accesspass.update_status()?;

    try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", accesspass);

    Ok(())
}
