use crate::{
    error::DoubleZeroError,
    helper::account_write,
    state::{
        accounttype::AccountType,
        multicastgroup::{MulticastGroup, MulticastGroupStatus},
        user::{User, UserStatus},
    },
};
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

    let multicastgroup_account = next_account_info(accounts_iter)?;
    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_subscribe_multicastgroup({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        multicastgroup_account.owner, program_id,
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
        multicastgroup_account.is_writable,
        "multicastgroup account is not writable"
    );
    assert!(user_account.is_writable, "user account is not writable");
    // Parse the global state account & check if the payer is in the allowlist
    //let _globalstate = globalstate_get(globalstate_account)?;

    // Parse accounts
    let mut mgroup: MulticastGroup = MulticastGroup::try_from(multicastgroup_account)?;
    assert_eq!(mgroup.account_type, AccountType::MulticastGroup);
    if mgroup.status != MulticastGroupStatus::Activated {
        #[cfg(test)]
        msg!("MulticastGroupStatus: {:?}", mgroup.status);

        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let mut user: User = User::try_from(user_account)?;
    assert_eq!(user.account_type, AccountType::User);
    if user.status != UserStatus::Activated && user.status != UserStatus::Updating {
        #[cfg(test)]
        msg!("UserStatus: {:?}", user.status);

        return Err(DoubleZeroError::InvalidStatus.into());
    }

    // Check if the user is in the allowlist
    if value.publisher && !mgroup.pub_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }
    if value.subscriber && !mgroup.sub_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    match value.publisher {
        true => {
            if !user.publishers.contains(multicastgroup_account.key) {
                user.publishers.push(*multicastgroup_account.key);
                user.status = UserStatus::Updating;
            }

            if !mgroup.publishers.contains(user_account.key) {
                mgroup.publishers.push(*user_account.key);
            }
        }
        false => {
            user.publishers
                .retain(|&x| x != *multicastgroup_account.key);
            mgroup.publishers.retain(|&x| x != *user_account.key);
        }
    }

    match value.subscriber {
        true => {
            if !user.subscribers.contains(multicastgroup_account.key) {
                user.subscribers.push(*multicastgroup_account.key);
                user.status = UserStatus::Updating;
            }

            if !mgroup.subscribers.contains(user_account.key) {
                mgroup.subscribers.push(*user_account.key);
            }
        }
        false => {
            user.subscribers
                .retain(|&x| x != *multicastgroup_account.key);
            mgroup.subscribers.retain(|&x| x != *user_account.key);
        }
    }

    account_write(
        multicastgroup_account,
        &mgroup,
        payer_account,
        system_program,
    );
    account_write(user_account, &user, payer_account, system_program);

    #[cfg(test)]
    msg!("Updated: {:?}", mgroup);
    #[cfg(test)]
    msg!("Updated: {:?}", user_account);

    Ok(())
}
