pub mod activate;
pub mod create;
pub mod delete;
pub mod finalize_retirement;
pub mod halt;
pub mod resume;
pub mod retire;
pub mod update;

use crate::{
    authorize::{authorize, FEED_ADMIN_FLAGS},
    error::{DoubleZeroError, Validate},
    state::{
        accesspass::AccessPass,
        bond_mirror::BondMirror,
        feed::{Feed, FeedStatus},
        globalstate::GlobalState,
        multicastgroup::MulticastGroup,
    },
};
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, msg, program_error::ProgramError,
    pubkey::Pubkey,
};

/// Split the accounts that name one of `groups` from the rest of an instruction's tail, by
/// address. A builder passes its groups' accounts so the program can read who owns them, and the
/// rest is the authorization tail, which [`authorize`] reads from its first account.
pub(crate) fn split_group_accounts<'a, 'b>(
    groups: &[Pubkey],
    tail: Vec<&'a AccountInfo<'b>>,
) -> (Vec<&'a AccountInfo<'b>>, Vec<&'a AccountInfo<'b>>) {
    tail.into_iter()
        .partition(|account| groups.contains(account.key))
}

/// Check that every group a bonded feed lists is its builder's own: created on the feed's bond, or
/// assigned to the builder by an admin with no bond.
///
/// A seat on a feed lets its holder join every group the feed lists ([`check_feed_metro_coverage`]),
/// so a feed the builder admits with its own bond may list only its own groups. A group with no bond
/// can only have been given to the builder by an admin, since a builder creates groups on a bond. A
/// group on another of the builder's bonds is refused, because each bond backs one feed. Each
/// group's account must be among `group_accounts`, in any order.
pub(crate) fn check_groups_on_bond(
    program_id: &Pubkey,
    builder: &Pubkey,
    bond_ref: &Pubkey,
    groups: &[Pubkey],
    group_accounts: &[&AccountInfo],
) -> ProgramResult {
    for group_key in groups {
        let Some(account) = group_accounts.iter().find(|a| a.key == group_key) else {
            msg!(
                "Multicast group {} has no account in the instruction",
                group_key
            );
            return Err(DoubleZeroError::GroupNotOnBond.into());
        };
        if account.owner != program_id {
            msg!(
                "Multicast group {} is not a serviceability account",
                group_key
            );
            return Err(DoubleZeroError::GroupNotOnBond.into());
        }
        let group =
            MulticastGroup::try_from(*account).map_err(|_| DoubleZeroError::GroupNotOnBond)?;
        let on_this_bond = group.bond_ref == *bond_ref || group.bond_ref == Pubkey::default();
        if group.owner != *builder || !on_this_bond {
            msg!(
                "Multicast group {} is owned by {} on bond {}, not by {} on bond {}",
                group_key,
                group.owner,
                group.bond_ref,
                builder,
                bond_ref
            );
            return Err(DoubleZeroError::GroupNotOnBond.into());
        }
    }
    Ok(())
}

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
/// `Retiring` admits, and that is the point of the state. RFC-29 gives seat holders thirty days
/// notice *before publication stops*, so a feed serves its existing holders throughout. Refusing
/// here would lock out anyone who reconnects on the first day of the notice, which would leave the
/// notice protecting only the population that cannot come back. A new seat is refused instead
/// where seats are provisioned, so nobody buys into a feed that is closing.
///
/// Call this where a seat is spent, never from the shared coverage check: `unsubscribe_feed` runs
/// through that too, and gating there would leave a user holding a seat on a retired feed with no
/// way to release it.
pub fn require_feed_admits(feed_key: &Pubkey, feed: &Feed) -> Result<(), DoubleZeroError> {
    if !matches!(feed.status, FeedStatus::Active | FeedStatus::Retiring) {
        msg!(
            "Feed {} is {}, so it admits no subscribers",
            feed_key,
            feed.status
        );
        return Err(DoubleZeroError::FeedNotActive);
    }
    Ok(())
}

/// Whether `payer` may change this feed's lifecycle.
///
/// Two ways in, for different reasons. The feed's own builder, because RFC-29 makes halt the
/// builder's lever and a builder that cannot halt its own feed cannot rotate its upstream source.
/// A `FEED_AUTHORITY` or `FOUNDATION` key, because a feed whose builder has gone quiet must still
/// be stoppable, and every other feed instruction already authorizes that way.
///
/// A catalog feed has no builder, so only the second way applies to it. The default pubkey is not
/// a signer anyone can produce, but the check is explicit rather than relying on that.
pub fn require_feed_writer<'a, 'b: 'a, I>(
    program_id: &Pubkey,
    accounts_iter: &mut I,
    payer: &Pubkey,
    globalstate: &GlobalState,
    feed: &Feed,
) -> ProgramResult
where
    I: Iterator<Item = &'a AccountInfo<'b>>,
{
    if feed.builder != Pubkey::default() && &feed.builder == payer {
        return Ok(());
    }

    authorize(
        program_id,
        accounts_iter,
        payer,
        globalstate,
        FEED_ADMIN_FLAGS,
    )
}

/// Whether the bond behind `feed` still covers the rate it publishes at.
///
/// Not the same question `CreateFeed` asks. Creation claims an unspent bond, so it requires
/// `feed_key` to be empty; here the feed already holds the claim, so the mirror must name this
/// feed and no other. What both check is the tier, because a mirror can be corrected downward
/// while a feed sits halted.
pub fn require_bond_still_covers(
    program_id: &Pubkey,
    mirror_account: &AccountInfo,
    feed_key: &Pubkey,
    feed: &Feed,
) -> Result<(), DoubleZeroError> {
    if mirror_account.data_is_empty() || mirror_account.owner != program_id {
        msg!("No bond mirror written for bond {}", feed.bond_ref);
        return Err(DoubleZeroError::BondMirrorMissing);
    }

    let mirror =
        BondMirror::try_from(mirror_account).map_err(|_| DoubleZeroError::InvalidAccountType)?;
    mirror.validate()?;

    if mirror.bond_ref != feed.bond_ref || mirror.builder != feed.builder {
        msg!("Bond mirror names a different bond or builder");
        return Err(DoubleZeroError::InvalidArgument);
    }
    // The claim has to point back at this feed. A mirror claimed by another feed is not this
    // feed's cover, whatever its tier says.
    if &mirror.feed_key != feed_key {
        msg!("Bond mirror is claimed by feed {}", mirror.feed_key);
        return Err(DoubleZeroError::InvalidArgument);
    }

    if !mirror.tier.covers(feed.committed_rate_bits_per_sec) {
        msg!(
            "Tier {} covers up to {} bits/sec, feed commits to {}",
            mirror.tier,
            mirror.tier.max_rate_bits_per_sec(),
            feed.committed_rate_bits_per_sec
        );
        return Err(DoubleZeroError::BondDoesNotCoverRate);
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    // Every status, because no instruction writes `Pending` any more: a `Pending` dropped from the
    // match would pass every test that drives real instructions.
    #[test]
    fn test_only_an_active_or_retiring_feed_admits() {
        for (status, admits) in [
            (FeedStatus::Pending, false),
            (FeedStatus::Active, true),
            (FeedStatus::Halted, false),
            (FeedStatus::Retiring, true),
            (FeedStatus::Retired, false),
        ] {
            let feed = Feed {
                status,
                ..Feed::default()
            };
            assert_eq!(
                require_feed_admits(&Pubkey::new_unique(), &feed).is_ok(),
                admits,
                "{status:?}"
            );
        }
    }
}
