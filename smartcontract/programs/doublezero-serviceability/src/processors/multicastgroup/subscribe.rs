use crate::{
    error::DoubleZeroError,
    helper::account_write,
    pda::get_accesspass_pda,
    state::{
        accesspass::AccessPass,
        multicastgroup::{MulticastGroup, MulticastGroupStatus},
        user::{User, UserStatus},
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};
use std::{fmt, net::Ipv4Addr};
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct MulticastGroupSubscribeArgs {
    pub client_ip: Ipv4Addr,
    pub publisher: bool,
    pub subscriber: bool,
}

impl fmt::Debug for MulticastGroupSubscribeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "client_ip: {}, publisher: {:?}, subscriber: {:?}",
            self.client_ip, self.publisher, self.subscriber
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
    let accesspass_account = next_account_info(accounts_iter)?;
    let user_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_subscribe_multicastgroup({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        mgroup_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid Accesspass Account Owner"
    );
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
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

    // Parse accounts
    let mut mgroup: MulticastGroup = MulticastGroup::try_from(mgroup_account)?;
    if mgroup.status != MulticastGroupStatus::Activated {
        #[cfg(test)]
        msg!("MulticastGroupStatus: {:?}", mgroup.status);

        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let mut user: User = User::try_from(user_account)?;
    if user.status != UserStatus::Activated && user.status != UserStatus::Updating {
        msg!("UserStatus: {:?}", user.status);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let (accesspass_pda, _) = get_accesspass_pda(program_id, &value.client_ip, payer_account.key);
    assert_eq!(
        accesspass_account.key, &accesspass_pda,
        "Invalid AccessPass PDA"
    );

    let accesspass = AccessPass::try_from(accesspass_account)?;
    assert!(
        accesspass.client_ip == user.client_ip,
        "AccessPass client_ip does not match"
    );

    // Check if the user is in the allowlist
    if value.publisher && !accesspass.mgroup_pub_allowlist.contains(mgroup_account.key) {
        msg!("{:?}", accesspass);
        return Err(DoubleZeroError::NotAllowed.into());
    }
    if value.subscriber && !accesspass.mgroup_sub_allowlist.contains(mgroup_account.key) {
        msg!("{:?}", accesspass);
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Manage the publisher lists
    match value.publisher {
        true => {
            if !user.publishers.contains(mgroup_account.key) {
                // Increment publisher count
                mgroup.publisher_count = mgroup.publisher_count.saturating_add(1);
                // Add multicast group to user's publisher list
                user.publishers.push(*mgroup_account.key);
                user.status = UserStatus::Updating;
            }
        }
        false => {
            if user.publishers.contains(mgroup_account.key) {
                // Decrement publisher count
                mgroup.publisher_count = mgroup.publisher_count.saturating_sub(1);
                // Remove multicast group from user's publisher list
                user.publishers.retain(|&x| x != *mgroup_account.key);
            }
        }
    }

    // Manage the subscriber lists
    match value.subscriber {
        true => {
            if !user.subscribers.contains(mgroup_account.key) {
                // Increment subscriber count
                mgroup.subscriber_count = mgroup.subscriber_count.saturating_add(1);
                // Add multicast group to user's subscriber list
                user.subscribers.push(*mgroup_account.key);
                user.status = UserStatus::Updating;
            }
        }
        false => {
            if user.subscribers.contains(mgroup_account.key) {
                // Decrement subscriber count
                mgroup.subscriber_count = mgroup.subscriber_count.saturating_sub(1);
                // Remove multicast group from user's subscriber list
                user.subscribers.retain(|&x| x != *mgroup_account.key);
            }
        }
    }

    account_write(mgroup_account, &mgroup, payer_account, system_program)?;
    account_write(user_account, &user, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Updated: {:?}", mgroup);
    #[cfg(test)]
    msg!("Updated: {:?}", user_account);

    Ok(())
}
