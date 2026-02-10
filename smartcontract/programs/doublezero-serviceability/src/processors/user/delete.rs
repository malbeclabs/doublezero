use crate::{
    error::DoubleZeroError,
    pda::get_accesspass_pda,
    serializer::try_acc_write,
    state::{
        accesspass::{AccessPass, AccessPassStatus},
        globalstate::GlobalState,
        user::*,
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
use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct UserDeleteArgs {}

impl fmt::Debug for UserDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_delete_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &UserDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_user({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid AccessPass Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(user_account.is_writable, "PDA Account is not writable");

    let mut user: User = User::try_from(user_account)?;

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key)
        && user.owner != *payer_account.key
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let (accesspass_pda, _) = get_accesspass_pda(program_id, &user.client_ip, &user.owner);
    let (accesspass_dynamic_pda, _) =
        get_accesspass_pda(program_id, &Ipv4Addr::UNSPECIFIED, &user.owner);
    // Access Pass must exist and match the client_ip or allow_multiple_ip must be enabled
    assert!(
        accesspass_account.key == &accesspass_pda
            || accesspass_account.key == &accesspass_dynamic_pda,
        "Invalid AccessPass PDA",
    );

    if !accesspass_account.data_is_empty() {
        // Read Access Pass
        let mut accesspass = AccessPass::try_from(accesspass_account)?;
        if accesspass.user_payer != user.owner {
            msg!(
                "Invalid user_payer accesspass.user_payer: {} = user_payer: {} ",
                accesspass.user_payer,
                user.owner
            );
            return Err(DoubleZeroError::Unauthorized.into());
        }
        if accesspass.is_dynamic() && accesspass.client_ip == Ipv4Addr::UNSPECIFIED {
            accesspass.client_ip = user.client_ip; // lock to the first used IP
        }
        if accesspass.client_ip != user.client_ip && !accesspass.allow_multiple_ip() {
            msg!(
                "Invalid client_ip accesspass.{{client_ip: {}}} = {{ client_ip: {} }}",
                accesspass.client_ip,
                user.client_ip
            );
            return Err(DoubleZeroError::Unauthorized.into());
        }

        accesspass.connection_count = accesspass.connection_count.saturating_sub(1);
        accesspass.status = if accesspass.connection_count > 0 {
            AccessPassStatus::Connected
        } else {
            AccessPassStatus::Disconnected
        };
        if accesspass.connection_count == 0 && accesspass.allow_multiple_ip() {
            accesspass.client_ip = Ipv4Addr::UNSPECIFIED; // reset to allow multiple IPs
        }

        try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;
    }

    if !user.publishers.is_empty() || !user.subscribers.is_empty() {
        msg!("{:?}", user);
        return Err(DoubleZeroError::ReferenceCountNotZero.into());
    }

    user.status = UserStatus::Deleting;

    try_acc_write(&user, user_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Deleting: {:?}", user);

    Ok(())
}
