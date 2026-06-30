pub mod create;
pub mod delete;
pub mod update;

use crate::{
    error::DoubleZeroError,
    state::{
        accesspass::AccessPass,
        feed::{Feed, FeedMetroMatch},
    },
};
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, msg, pubkey::Pubkey};

/// Validate the EdgeSeat feed metro gate without mutating the pass.
///
/// For the device's `device_exchange`, the joinable groups are the matching feed's group set; a
/// device in an exchange not covered by any of the pass's feeds is rejected with `MetroMismatch`. A
/// feed with no metros imposes no restriction (reachable from any exchange, any group).
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
    let feed_account = feed_account.ok_or(DoubleZeroError::MetroMismatch)?;
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
        return Err(DoubleZeroError::MetroMismatch.into());
    }

    match feed.groups_for(device_exchange) {
        FeedMetroMatch::Unrestricted => {}
        FeedMetroMatch::Groups(groups) => {
            if let Some(group) = target_mgroup {
                if !groups.contains(group) {
                    msg!(
                        "Group {} not joinable for exchange {} via feed {}",
                        group,
                        device_exchange,
                        feed_account.key
                    );
                    return Err(DoubleZeroError::MetroMismatch.into());
                }
            }
        }
        FeedMetroMatch::NotCovered => {
            msg!(
                "Device exchange {} not covered by feed {}",
                device_exchange,
                feed_account.key
            );
            return Err(DoubleZeroError::MetroMismatch.into());
        }
    }

    Ok(())
}

/// Enforce the EdgeSeat feed metro gate at connect and tick the matching feed seat against its cap.
/// Call only for EdgeSeat passes. See [`check_feed_metro_coverage`].
pub fn enforce_feed_metro_gate(
    program_id: &Pubkey,
    accesspass: &mut AccessPass,
    device_exchange: &Pubkey,
    target_mgroup: Option<&Pubkey>,
    feed_account: Option<&AccountInfo>,
) -> ProgramResult {
    check_feed_metro_coverage(
        program_id,
        accesspass,
        device_exchange,
        target_mgroup,
        feed_account,
    )?;
    // feed_account is guaranteed Some here (check returns Err otherwise).
    if let Some(feed_account) = feed_account {
        accesspass.try_add_feed_user(feed_account.key)?;
    }
    Ok(())
}
