use crate::{
    error::DoubleZeroError,
    pda::get_accesspass_pda,
    serializer::try_acc_write,
    state::{
        accesspass::AccessPass,
        multicastgroup::{MulticastGroup, MulticastGroupStatus},
        user::{User, UserStatus},
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::{fmt, net::Ipv4Addr};
#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct MulticastGroupSubscribeArgs {
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
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

pub struct SubscribeUserResult {
    pub mgroup: MulticastGroup,
    /// True if the publisher list transitioned between empty and non-empty
    /// (gained first publisher or lost last publisher). Callers that need to
    /// trigger activator reprocessing should check this flag.
    pub publisher_list_transitioned: bool,
}

/// Toggle a user's multicast group subscription.
///
/// Handles both create-time subscription (user lists start empty, only adds)
/// and post-activation subscription changes (add/remove toggle). The caller is
/// responsible for setting `user.status = Updating` when
/// `publisher_list_transitioned` is true and the user is already activated.
pub fn subscribe_user_to_multicastgroup(
    mgroup_account: &AccountInfo,
    accesspass: &AccessPass,
    user: &mut User,
    publisher: bool,
    subscriber: bool,
) -> Result<SubscribeUserResult, ProgramError> {
    let mut mgroup = MulticastGroup::try_from(mgroup_account)?;
    if mgroup.status != MulticastGroupStatus::Activated {
        msg!("MulticastGroupStatus: {:?}", mgroup.status);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    // Check allowlists for additions
    if publisher && !accesspass.mgroup_pub_allowlist.contains(mgroup_account.key) {
        msg!("{:?}", accesspass);
        return Err(DoubleZeroError::NotAllowed.into());
    }
    if subscriber && !accesspass.mgroup_sub_allowlist.contains(mgroup_account.key) {
        msg!("{:?}", accesspass);
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut publisher_list_transitioned = false;

    // Manage the publisher list
    match publisher {
        true => {
            if !user.publishers.contains(mgroup_account.key) {
                let was_empty = user.publishers.is_empty();
                mgroup.publisher_count = mgroup.publisher_count.saturating_add(1);
                user.publishers.push(*mgroup_account.key);
                if was_empty {
                    publisher_list_transitioned = true;
                }
            }
        }
        false => {
            if user.publishers.contains(mgroup_account.key) {
                mgroup.publisher_count = mgroup.publisher_count.saturating_sub(1);
                user.publishers.retain(|&x| x != *mgroup_account.key);
                if user.publishers.is_empty() {
                    publisher_list_transitioned = true;
                }
            }
        }
    }

    // Manage the subscriber list
    match subscriber {
        true => {
            if !user.subscribers.contains(mgroup_account.key) {
                mgroup.subscriber_count = mgroup.subscriber_count.saturating_add(1);
                user.subscribers.push(*mgroup_account.key);
            }
        }
        false => {
            if user.subscribers.contains(mgroup_account.key) {
                mgroup.subscriber_count = mgroup.subscriber_count.saturating_sub(1);
                user.subscribers.retain(|&x| x != *mgroup_account.key);
            }
        }
    }

    Ok(SubscribeUserResult {
        mgroup,
        publisher_list_transitioned,
    })
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

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

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
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    assert!(
        mgroup_account.is_writable,
        "multicastgroup account is not writable"
    );
    assert!(user_account.is_writable, "user account is not writable");

    // Parse and validate user
    let mut user: User = User::try_from(user_account)?;
    if user.status != UserStatus::Activated && user.status != UserStatus::Updating {
        msg!("UserStatus: {:?}", user.status);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let accesspass = AccessPass::try_from(accesspass_account)?;

    let ip_seed = if accesspass.allow_multiple_ip() {
        Ipv4Addr::UNSPECIFIED
    } else {
        user.client_ip
    };

    let (accesspass_pda, _) = get_accesspass_pda(program_id, &ip_seed, payer_account.key);
    assert_eq!(
        accesspass_account.key, &accesspass_pda,
        "Invalid AccessPass PDA",
    );

    if !accesspass.allow_multiple_ip() && accesspass.client_ip != user.client_ip {
        msg!(
            "AccessPass client_ip does not match. accesspass.client_ip: {} user.client_ip: {}",
            accesspass.client_ip,
            user.client_ip
        );
        return Err(DoubleZeroError::Unauthorized.into());
    }

    let result = subscribe_user_to_multicastgroup(
        mgroup_account,
        &accesspass,
        &mut user,
        value.publisher,
        value.subscriber,
    )?;

    // Trigger activator reprocessing when publisher list transitions
    // (gaining first publisher requires dz_ip allocation, losing last means it's no longer needed)
    if result.publisher_list_transitioned {
        user.status = UserStatus::Updating;
    }

    try_acc_write(&result.mgroup, mgroup_account, payer_account, accounts)?;
    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}
