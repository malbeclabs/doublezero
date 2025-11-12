use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::account_write,
    pda::get_accesspass_pda,
    state::{
        accesspass::{AccessPass, AccessPassStatus},
        accounttype::AccountTypeInfo,
        user::{User, UserStatus},
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::resize_account::resize_account_if_needed;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct CheckUserAccessPassArgs {}

impl fmt::Debug for CheckUserAccessPassArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_check_access_pass_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &CheckUserAccessPassArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_check_access_pass_user({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid AccessPass Account Owner"
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
    assert!(user_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user: User = User::try_from(user_account)?;

    let (accesspass_pda, _) = get_accesspass_pda(program_id, &user.client_ip, &user.owner);
    assert_eq!(
        accesspass_account.key, &accesspass_pda,
        "Invalid AccessPass PDA"
    );

    // Read Access Pass
    let mut accesspass = AccessPass::try_from(accesspass_account)?;
    accesspass.update_status()?;

    if user.status != UserStatus::Activated && user.status != UserStatus::OutOfCredits {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    user.status = if accesspass.status == AccessPassStatus::Expired {
        UserStatus::OutOfCredits
    } else {
        UserStatus::Activated
    };

    account_write(user_account, &user, payer_account, system_program)?;
    resize_account_if_needed(
        accesspass_account,
        payer_account,
        accounts,
        accesspass.size(),
    )?;
    accesspass.try_serialize(accesspass_account)?;

    #[cfg(test)]
    msg!("OutOfCredits: {:?}", user);

    Ok(())
}
