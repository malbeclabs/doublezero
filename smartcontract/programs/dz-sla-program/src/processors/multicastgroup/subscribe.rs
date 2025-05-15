use crate::globalstate::globalstate_get;
use crate::helper::*;
use crate::state::accounttype::AccountType;
use crate::state::multicastgroup::*;
use crate::state::user::User;
use crate::state::user::*;
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct MulticastGroupSubscribeArgs {
    pub publisher: bool,
    pub subscriber: bool,
}

impl fmt::Debug for MulticastGroupSubscribeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "publisher: {:?}, subscriber: {:?}",
            self.publisher, self.subscriber
        )
    }
}

pub fn process_subscribe_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupSubscribeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_subscribe_multicastgroup({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        mgroup_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    assert!(
        mgroup_account.is_writable,
        "multicastgroup account is not writable"
    );
    assert!(user_account.is_writable, "user account is not writable");
    // Parse the global state account & check if the payer is in the allowlist
    let _globalstate = globalstate_get(globalstate_account)?;

    //TODO: Check if payer is in the allowlist

    // Parse accounts
    let mut mgroup: MulticastGroup = MulticastGroup::from(mgroup_account);
    assert_eq!(mgroup.account_type, AccountType::MulticastGroup);
    assert_eq!(mgroup.status, MulticastGroupStatus::Activated);

    let mut user = User::from(user_account);
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.status, UserStatus::Activated);

    match value.publisher {
        true => {
            if !user.publishers.contains(&mgroup_account.key) {
                user.publishers.push(*mgroup_account.key);
                user.status = UserStatus::Pending;
            }

            if !mgroup.publishers.contains(&user_account.key) {
                mgroup.publishers.push(*user_account.key);
            }
        }
        false => {
            user.publishers.retain(|&x| x != *mgroup_account.key);
            mgroup.publishers.retain(|&x| x != *user_account.key);
        }
    }

    match value.subscriber {
        true => {
            if !user.subscribers.contains(&mgroup_account.key) {
                user.subscribers.push(*mgroup_account.key);
                user.status = UserStatus::Pending;
            }

            if !mgroup.subscribers.contains(&user_account.key) {
                mgroup.subscribers.push(*user_account.key);
            }
        }
        false => {
            user.subscribers.retain(|&x| x != *mgroup_account.key);
            mgroup.subscribers.retain(|&x| x != *user_account.key);
        }
    }

    account_write(mgroup_account, &mgroup, payer_account, system_program);
    account_write(user_account, &user, payer_account, system_program);

    #[cfg(test)]
    msg!("Updated: {:?}", mgroup);
    #[cfg(test)]
    msg!("Updated: {:?}", user_account);

    Ok(())
}
