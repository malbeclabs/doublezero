//! `UnsubscribeFeed` (variant 118) — leave whole feeds on an EdgeSeat access pass.
//!
//! Accounts:
//! `[accesspass, user, globalstate, device, targets.., retained.., groups.., payer, system, perm?]`.
//!
//! `retained` is every feed the user keeps. It is required, not optional: two feeds on one pass can
//! carry the same group, so without the retained feeds' group sets a departing feed would drop a
//! group another held feed still covers, and leave that feed's seat charged against a user holding
//! nothing in it. See [`super::feed`] for the shared model.

use crate::{
    error::DoubleZeroError,
    processors::multicastgroup::feed::{
        apply_groups, check_group_accounts, load_context, load_feeds, write_back,
    },
    state::permission::permission_flags,
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, msg, pubkey::Pubkey};
use std::fmt;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct UnsubscribeFeedArgs {
    /// Feeds being left.
    pub feed_count: u8,
    /// Feeds the user keeps. Together with `feed_count` this must cover every feed the user holds.
    pub retained_feed_count: u8,
}

impl fmt::Debug for UnsubscribeFeedArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "feed_count: {:?}, retained_feed_count: {:?}",
            self.feed_count, self.retained_feed_count
        )
    }
}

pub fn process_unsubscribe_feed(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UnsubscribeFeedArgs,
) -> ProgramResult {
    let mut ctx = load_context(program_id, accounts)?;

    #[cfg(test)]
    msg!("process_unsubscribe_feed({:?})", value);

    // No status check: a user created but not yet activated must still be cleanable.
    ctx.authorize_payer(
        program_id,
        permission_flags::USER_ADMIN,
        DoubleZeroError::NotAllowed,
    )?;

    let feed_count = value.feed_count as usize;
    let retained_count = value.retained_feed_count as usize;
    if feed_count == 0 || ctx.variable.len() < feed_count + retained_count {
        msg!(
            "bad account shape: feed_count={}, retained_feed_count={}, variable={}",
            feed_count,
            retained_count,
            ctx.variable.len()
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    let (target_accounts, rest) = ctx.variable.split_at(feed_count);
    let (retained_accounts, group_accounts) = rest.split_at(retained_count);

    let targets = load_feeds(
        program_id,
        &ctx.accesspass,
        &ctx.device.exchange_pk,
        target_accounts,
    )?;
    let retained = load_feeds(
        program_id,
        &ctx.accesspass,
        &ctx.device.exchange_pk,
        retained_accounts,
    )?;

    // Without both checks a caller can release a seat while keeping the groups.
    for (key, _) in &retained {
        if targets.iter().any(|(target, _)| target == key) {
            msg!("feed {} passed as both a target and retained", key);
            return Err(DoubleZeroError::InvalidArgument.into());
        }
        if !ctx.user.feed_pks.contains(key) {
            msg!("retained feed {} is not held by this user", key);
            return Err(DoubleZeroError::InvalidArgument.into());
        }
    }

    for held in &ctx.user.feed_pks {
        if !targets.iter().any(|(key, _)| key == held)
            && !retained.iter().any(|(key, _)| key == held)
        {
            msg!("feed {} is held by this user and must be passed", held);
            return Err(DoubleZeroError::FeedAccountRequired.into());
        }
    }

    let mut expected: Vec<Pubkey> = Vec::new();
    for (_, feed) in &targets {
        for group in &feed.groups {
            let still_covered = retained.iter().any(|(_, r)| r.groups.contains(group));
            if ctx.user.subscribers.contains(group) && !still_covered && !expected.contains(group) {
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
        false,
    )?;

    for (feed_key, _) in &targets {
        if ctx.user.feed_pks.contains(feed_key) {
            ctx.accesspass.remove_feed_user(feed_key);
            ctx.user.feed_pks.retain(|k| k != feed_key);
        }
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
