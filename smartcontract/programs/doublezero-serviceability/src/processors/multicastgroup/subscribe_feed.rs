//! `UpdateFeedSubscription` (variant 117) — join or leave every multicast group carried by one or
//! more feeds on an EdgeSeat access pass, in a single atomic transaction.
//!
//! A feed is the SKU an EdgeSeat pass holder buys: one metro, one group set, one seat cap. This
//! instruction is the only path that charges those seats, which is what keeps the accounting honest:
//! [`super::subscribe`] handles allowlist-granted (comped) groups and never touches a seat.
//!
//! Seats are held per *feed*, not per group. Three groups inside one feed cost one seat, and a
//! second feed on the same pass costs a second. The reconciliation below derives seat state from the
//! user's final group membership rather than ticking incrementally, so adds, partial removals and
//! full removals all fall out of the same comparison.
//!
//! A feed is receive-only, so there is no publisher flag. Any publisher role the user already holds
//! on a group is carried through untouched — stripping it here would deallocate the user's `dz_ip`
//! as a side effect of a subscribe.

use crate::{
    authorize::{authorize, split_trailing_permission},
    error::DoubleZeroError,
    pda::{get_accesspass_pda, get_globalstate_pda},
    processors::{
        accesspass::set_feeds::MAX_ACCESS_PASS_FEEDS, feed::check_feed_metro_coverage,
        validation::validate_program_account,
    },
    serializer::try_acc_write,
    state::{
        accesspass::{AccessPass, AccessPassType},
        device::Device,
        feed::Feed,
        globalstate::GlobalState,
        permission::permission_flags,
        user::{User, UserStatus, UserType},
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};
use std::{fmt, net::Ipv4Addr};

use super::subscribe::update_user_multicastgroup_roles;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct UpdateFeedSubscriptionArgs {
    /// `true` joins every passed group, `false` leaves them.
    pub subscriber: bool,
    /// How many of the variable accounts are Feeds. The rest are MulticastGroups.
    pub feed_count: u8,
}

impl fmt::Debug for UpdateFeedSubscriptionArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "subscriber: {:?}, feed_count: {:?}",
            self.subscriber, self.feed_count
        )
    }
}

pub fn process_update_feed_subscription(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UpdateFeedSubscriptionArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    // Account layout:
    //   [accesspass, user, globalstate, device,
    //    feed_0..feed_{F-1}, group_0..group_{G-1},
    //    payer, system, permission?]
    // F is `feed_count`; G is whatever remains of the variable section. The device is fixed rather
    // than optional because every path through this instruction needs it: the metro check compares
    // each feed against the device's exchange.
    let accesspass_account = next_account_info(accounts_iter)?;
    let user_account = next_account_info(accounts_iter)?;
    let gs_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;

    let remaining: Vec<&AccountInfo> = accounts_iter.collect();
    let (payer_account, system_program, variable, permission_account) =
        split_trailing_permission(program_id, &remaining)?;

    #[cfg(test)]
    msg!("process_update_feed_subscription({:?})", value);

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    let (expected_globalstate_pda, _) = get_globalstate_pda(program_id);
    assert_eq!(
        gs_account.key, &expected_globalstate_pda,
        "Invalid GlobalState PDA"
    );
    let globalstate = GlobalState::try_from(gs_account)?;

    // The pass is written here (seat counts move), unlike the allowlist path which reads it.
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    validate_program_account!(
        accesspass_account,
        program_id,
        writable = true,
        "AccessPass"
    );
    validate_program_account!(user_account, program_id, writable = true, "User");
    validate_program_account!(device_account, program_id, writable = false, "Device");

    let feed_count = value.feed_count as usize;
    if feed_count == 0 || variable.len() <= feed_count {
        msg!(
            "expected at least one Feed and one MulticastGroup account (feed_count={}, variable={})",
            feed_count,
            variable.len()
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    let (feed_accounts, group_accounts) = variable.split_at(feed_count);

    let mut user: User = User::try_from(user_account)?;
    // Only a multicast user occupies a feed seat, so no other type may hold a feed subscription.
    if user.user_type != UserType::Multicast {
        msg!(
            "A feed subscription requires a Multicast user, got {}",
            user.user_type
        );
        return Err(DoubleZeroError::EdgeSeatIsMulticastOnly.into());
    }
    // Leaving is allowed from any status so a user created but not yet activated can be cleaned up.
    if value.subscriber && user.status != UserStatus::Activated {
        msg!("UserStatus: {:?}", user.status);
        return Err(DoubleZeroError::InvalidStatus.into());
    }
    if device_account.key != &user.device_pk {
        msg!(
            "Device {} is not the user's device {}",
            device_account.key,
            user.device_pk
        );
        return Err(DoubleZeroError::UserDeviceMismatch.into());
    }
    let device = Device::try_from(device_account)?;

    let mut accesspass = AccessPass::try_from(accesspass_account)?;
    let (accesspass_pda, _) = get_accesspass_pda(program_id, &user.client_ip, &user.owner);
    let (accesspass_dynamic_pda, _) =
        get_accesspass_pda(program_id, &Ipv4Addr::UNSPECIFIED, &user.owner);
    assert!(
        accesspass_account.key == &accesspass_pda
            || accesspass_account.key == &accesspass_dynamic_pda,
        "Invalid AccessPass PDA",
    );
    if !matches!(accesspass.accesspass_type, AccessPassType::EdgeSeat(_)) {
        msg!(
            "AccessPass type {:?} carries no feeds; use UpdateMulticastGroupRoles",
            accesspass.accesspass_type
        );
        return Err(DoubleZeroError::EdgeSeatRequired.into());
    }

    // Authorization mirrors UpdateMulticastGroupRoles: the pass's own user_payer, a foundation
    // member, or a permission holder. Joining consumes the pass's paid capacity, so it is an
    // ACCESS_PASS_ADMIN operation; leaving is cleanup a USER_ADMIN may perform as a prerequisite to
    // deleting the user.
    if accesspass.user_payer != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        let required_flag = if value.subscriber {
            permission_flags::ACCESS_PASS_ADMIN
        } else {
            permission_flags::USER_ADMIN
        };
        if authorize(
            program_id,
            &mut permission_account.into_iter(),
            payer_account.key,
            &globalstate,
            required_flag,
        )
        .is_err()
        {
            msg!(
                "AccessPass user_payer {:?} does not match payer {:?}",
                accesspass.user_payer,
                payer_account.key
            );
            return Err(if value.subscriber {
                DoubleZeroError::Unauthorized.into()
            } else {
                DoubleZeroError::NotAllowed.into()
            });
        }
    }

    // Validate every feed up front, and collect their group sets for the membership check below.
    // check_feed_metro_coverage enforces that the feed is provisioned on this pass and serves the
    // device's metro; passing None for the group defers the per-group check to the loop after it,
    // since a group need only be carried by *one* of the passed feeds.
    let mut feeds: Vec<(Pubkey, Feed)> = Vec::with_capacity(feed_accounts.len());
    for feed_account in feed_accounts {
        check_feed_metro_coverage(
            program_id,
            &accesspass,
            &device.exchange_pk,
            None,
            Some(feed_account),
        )?;
        if feeds.iter().any(|(key, _)| key == feed_account.key) {
            msg!("Feed {} passed more than once", feed_account.key);
            return Err(DoubleZeroError::InvalidArgument.into());
        }
        feeds.push((*feed_account.key, Feed::try_from(*feed_account)?));
    }

    // Apply the role change per group. The publisher role is carried through unchanged: a feed sells
    // receive only, and clearing it here would deallocate the user's dz_ip as a side effect.
    for group_account in group_accounts {
        validate_program_account!(
            *group_account,
            program_id,
            writable = true,
            "MulticastGroup"
        );
        if !feeds
            .iter()
            .any(|(_, feed)| feed.groups.contains(group_account.key))
        {
            msg!(
                "Group {} is not carried by any of the passed feeds",
                group_account.key
            );
            return Err(DoubleZeroError::GroupNotInFeed.into());
        }
        let carry_publisher = user.publishers.contains(group_account.key);
        let result = update_user_multicastgroup_roles(
            group_account,
            &mut user,
            carry_publisher,
            value.subscriber,
        )?;
        try_acc_write(&result.mgroup, group_account, payer_account, accounts)?;
    }

    // Reconcile seats against the user's *final* membership rather than ticking as we go. A feed is
    // held exactly while the user is in at least one of its groups, so a second group inside a held
    // feed is free, dropping one of several groups keeps the seat, and dropping the last releases it.
    let held_groups = user.get_multicast_groups();
    for (feed_key, feed) in &feeds {
        let holds_group = feed.groups.iter().any(|group| held_groups.contains(group));
        let seat_recorded = user.feed_pks.contains(feed_key);

        if holds_group && !seat_recorded {
            if user.feed_pks.len() >= MAX_ACCESS_PASS_FEEDS {
                return Err(DoubleZeroError::UserFeedLimitExceeded.into());
            }
            accesspass.try_add_feed_user(feed_key)?;
            user.feed_pks.push(*feed_key);
        } else if !holds_group && seat_recorded {
            accesspass.remove_feed_user(feed_key);
            user.feed_pks.retain(|held| held != feed_key);
        }
    }

    try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;
    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}
