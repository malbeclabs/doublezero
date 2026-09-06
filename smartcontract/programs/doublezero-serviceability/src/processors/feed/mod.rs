pub mod create;
pub mod delete;
pub mod update;

use crate::{
    error::DoubleZeroError,
    state::{
        accesspass::AccessPass,
        feed::{Feed, FeedStatus},
    },
};
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, msg, program_error::ProgramError,
    pubkey::Pubkey,
};

/// Validate the EdgeSeat feed metro gate without mutating the pass.
///
/// A feed serves exactly one metro. The passed feed must be provisioned on the pass and must serve
/// the device's `device_exchange` (else `MetroMismatch`); the joinable multicast groups are then
/// that feed's group set (a `target_mgroup` outside it is rejected with `GroupNotInFeed`).
///
/// `feed_account` must be the `Feed` referenced by one of the pass's seats. `target_mgroup` is the
/// multicast group being joined (None requires only metro coverage).
pub fn check_feed_metro_coverage(
    program_id: &Pubkey,
    accesspass: &AccessPass,
    device_exchange: &Pubkey,
    target_mgroup: Option<&Pubkey>,
    feed_account: Option<&AccountInfo>,
) -> ProgramResult {
    let feed_account = feed_account.ok_or(DoubleZeroError::FeedAccountRequired)?;
    if feed_account.owner != program_id {
        return Err(DoubleZeroError::InvalidAccountOwner.into());
    }
    let feed = Feed::try_from(feed_account)?;

    // The feed must be one provisioned onto this pass.
    if !accesspass
        .feed_seats()
        .iter()
        .any(|s| s.feed_key == *feed_account.key)
    {
        msg!(
            "Feed {} is not provisioned on the access pass",
            feed_account.key
        );
        return Err(DoubleZeroError::FeedNotOnAccessPass.into());
    }

    // The feed serves exactly one metro; it must be the device's exchange.
    if feed.exchange != *device_exchange {
        msg!(
            "Device exchange {} not served by feed {} (serves {})",
            device_exchange,
            feed_account.key,
            feed.exchange
        );
        return Err(DoubleZeroError::MetroMismatch.into());
    }

    if let Some(group) = target_mgroup {
        if !feed.groups.contains(group) {
            msg!(
                "Group {} not joinable for exchange {} via feed {}",
                group,
                device_exchange,
                feed_account.key
            );
            return Err(DoubleZeroError::GroupNotInFeed.into());
        }
    }

    Ok(())
}

/// Enforce the EdgeSeat feed metro gate at connect and tick the matching feed seat against its cap.
/// Call only for EdgeSeat passes. See [`check_feed_metro_coverage`].
/// Returns the `feed_key` whose seat was ticked, so the caller can record it on the User and
/// release exactly that seat at disconnect.
pub fn enforce_feed_metro_gate(
    program_id: &Pubkey,
    accesspass: &mut AccessPass,
    device_exchange: &Pubkey,
    target_mgroup: Option<&Pubkey>,
    feed_account: Option<&AccountInfo>,
) -> Result<Pubkey, ProgramError> {
    check_feed_metro_coverage(
        program_id,
        accesspass,
        device_exchange,
        target_mgroup,
        feed_account,
    )?;
    // feed_account is guaranteed Some here (check returns Err otherwise).
    let feed_account = feed_account.ok_or(DoubleZeroError::FeedAccountRequired)?;
    require_feed_admits(feed_account.key, &Feed::try_from(feed_account)?)?;
    accesspass.try_add_feed_user(feed_account.key)?;
    Ok(*feed_account.key)
}

/// Reject a feed that is not publishing, before a seat is spent on it.
///
/// This is the whole enforcement of feed lifecycle on the subscriber side. A feed that is pending
/// conformance, halted by its builder, or retired stops admitting subscribers the moment its
/// status changes, so retirement and slashing need no sweep over the access passes that already
/// carry a seat for it.
///
/// Call this where a seat is spent, never from the shared coverage check: `unsubscribe_feed` runs
/// through that too, and gating there would leave a user holding a seat on a retired feed with no
/// way to release it.
pub fn require_feed_admits(feed_key: &Pubkey, feed: &Feed) -> Result<(), DoubleZeroError> {
    if feed.status != FeedStatus::Active {
        msg!("Feed {} is {}, so it admits no subscribers", feed_key, feed.status);
        return Err(DoubleZeroError::FeedNotActive);
    }
    Ok(())
}
