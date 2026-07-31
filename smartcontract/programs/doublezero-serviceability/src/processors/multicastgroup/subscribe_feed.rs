//! `SubscribeFeed` (variant 117) — join whole feeds on an EdgeSeat access pass.
//!
//! Accounts: `[accesspass, user, globalstate, device, feeds.., groups.., payer, system, perm?]`.
//! One seat is charged per feed newly held. See [`super::feed`] for the shared model.

use crate::{
    error::DoubleZeroError,
    processors::multicastgroup::feed::{
        apply_groups, check_group_accounts, load_context, load_feeds, write_back,
    },
    state::{permission::permission_flags, user::UserStatus},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, msg, pubkey::Pubkey};
use std::fmt;

/// Cap on the feeds one user may hold at once. Bounded by what a single [`UnsubscribeFeed`]
/// transaction can name: every held feed plus up to `MAX_FEED_GROUPS` departing groups, behind
/// the client's compute-budget prelude, fits a legacy transaction only up to 25 such accounts.
pub const MAX_USER_FEEDS: usize = 5;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct SubscribeFeedArgs {
    /// How many of the variable accounts are Feeds. The rest are the MulticastGroups being joined.
    pub feed_count: u8,
}

impl fmt::Debug for SubscribeFeedArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "feed_count: {:?}", self.feed_count)
    }
}

pub fn process_subscribe_feed(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SubscribeFeedArgs,
) -> ProgramResult {
    let mut ctx = load_context(program_id, accounts)?;

    #[cfg(test)]
    msg!("process_subscribe_feed({:?})", value);

    if ctx.user.status != UserStatus::Activated {
        msg!("UserStatus: {:?}", ctx.user.status);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    // Joining consumes the pass's paid capacity.
    ctx.authorize_payer(
        program_id,
        permission_flags::ACCESS_PASS_ADMIN,
        DoubleZeroError::Unauthorized,
    )?;

    let feed_count = value.feed_count as usize;
    if feed_count == 0 || ctx.variable.len() < feed_count {
        msg!(
            "bad account shape: feed_count={}, variable={}",
            feed_count,
            ctx.variable.len()
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    let (feed_accounts, group_accounts) = ctx.variable.split_at(feed_count);

    let feeds = load_feeds(
        program_id,
        &ctx.accesspass,
        &ctx.device.exchange_pk,
        feed_accounts,
        &[],
    )?;

    let mut expected: Vec<Pubkey> = Vec::new();
    for (_, feed) in &feeds {
        for group in &feed.groups {
            if !ctx.user.subscribers.contains(group) && !expected.contains(group) {
                expected.push(*group);
            }
        }
    }
    check_group_accounts(group_accounts, &expected)?;

    apply_groups(
        program_id,
        accounts,
        group_accounts,
        &mut ctx.user,
        ctx.payer_account,
        true,
    )?;

    for (feed_key, _) in &feeds {
        if ctx.user.feed_pks.contains(feed_key) {
            continue;
        }
        if ctx.user.feed_pks.len() >= MAX_USER_FEEDS {
            msg!(
                "user already holds {} feeds; a user may hold at most {}",
                ctx.user.feed_pks.len(),
                MAX_USER_FEEDS
            );
            return Err(DoubleZeroError::UserFeedLimitExceeded.into());
        }
        ctx.accesspass.try_add_feed_user(feed_key)?;
        ctx.user.feed_pks.push(*feed_key);
    }

    write_back(
        &ctx.accesspass,
        &ctx.user,
        ctx.accesspass_account,
        ctx.user_account,
        ctx.payer_account,
        accounts,
    )
}
