//! Keeps EdgeSeat multicast subscriptions inside the feeds on the subscriber's access pass.
//!
//! An EdgeSeat user's multicast groups must always be a subset of the groups carried by the feeds
//! seated on their access pass, in their device's metro. Serviceability relies on that invariant
//! when it decides whether an unsubscribe releases a feed's seat. `UpdateFeed` and `DeleteFeed`
//! can break it: dropping a group the feed used to carry leaves every subscriber of that group
//! holding a membership outside their feeds, and the program cannot notice — there is no reverse
//! index from a `Feed` to its subscribers.
//!
//! So the admin side closes the hole: before the feed change is submitted, unsubscribe the users
//! it would orphan. Unsubscribe-then-rotate only ever passes through legal states, since a user
//! holding fewer groups than their feeds offer is fine. Rotate-then-unsubscribe would publish the
//! illegal state for as long as the cleanup takes.

use crate::doublezerocommand::CliCommand;
use doublezero_sdk::{
    commands::{
        accesspass::list::ListAccessPassCommand, device::list::ListDeviceCommand,
        feed::list::ListFeedCommand, multicastgroup::subscribe::UpdateMulticastGroupRolesCommand,
        user::list::ListUserCommand,
    },
    Device, Feed, User,
};
use doublezero_serviceability::state::accesspass::{AccessPass, AccessPassType};
use eyre::Context;
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, io::Write, net::Ipv4Addr};

/// Unsubscribe passes to attempt before giving up on a population that keeps changing underneath
/// the scan. Each pass is preceded by a fresh scan, so a clean scan ends the loop early.
const MAX_UNSUBSCRIBE_ROUNDS: usize = 3;

/// A multicast-group membership the pending feed change would leave outside the user's feeds.
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
pub struct Orphan {
    pub user_pk: Pubkey,
    pub group_pk: Pubkey,
    pub client_ip: Ipv4Addr,
    /// The user holds the publisher role on this group. EdgeSeat is meant to be subscribe-only, so
    /// this is worth calling out in the plan an operator reads before forcing the change through.
    pub publisher: bool,
    pub subscriber: bool,
}

/// The onchain state [`plan`] reads, fetched once per round by [`scan`].
pub struct Snapshot {
    pub users: HashMap<Pubkey, User>,
    pub accesspasses: HashMap<Pubkey, AccessPass>,
    pub devices: HashMap<Pubkey, Device>,
    pub feeds: HashMap<Pubkey, Feed>,
}

/// Unsubscribes every EdgeSeat user that dropping `dropped` from `feed_pk` would orphan, leaving
/// the caller to submit the feed change afterwards.
///
/// Without `force` this only reports: a non-empty plan fails the command before anything is
/// submitted. With `force` the removals are applied and re-verified against a fresh scan.
/// Removals are idempotent, so a run that fails partway is safe to repeat.
pub fn unsubscribe_orphans<C: CliCommand, W: Write>(
    client: &C,
    out: &mut W,
    feed_pk: &Pubkey,
    feed_code: &str,
    dropped: &[Pubkey],
    force: bool,
) -> eyre::Result<()> {
    if dropped.is_empty() {
        return Ok(());
    }

    let mut rounds_done = 0;
    loop {
        let orphans = plan(feed_pk, dropped, &scan(client)?)?;
        if orphans.is_empty() {
            return Ok(());
        }

        report(out, feed_code, &orphans, rounds_done)?;

        if !force {
            eyre::bail!(
                "refusing to change feed '{feed_code}': {} multicast subscription(s) would be left \
                 outside their access pass's feeds. Re-run with --force-unsubscribe to unsubscribe \
                 them first.",
                orphans.len()
            );
        }
        if rounds_done == MAX_UNSUBSCRIBE_ROUNDS {
            eyre::bail!(
                "new subscriptions kept appearing across {MAX_UNSUBSCRIBE_ROUNDS} unsubscribe \
                 round(s) on feed '{feed_code}'; nothing was changed on the feed. Re-run once \
                 subscriptions to its groups have quiesced."
            );
        }

        for orphan in &orphans {
            client
                .update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                    user_pk: orphan.user_pk,
                    group_pk: orphan.group_pk,
                    client_ip: orphan.client_ip,
                    publisher: false,
                    subscriber: false,
                    device_pk: None,
                    feed_pk: None,
                })
                .wrap_err_with(|| {
                    format!(
                        "failed to unsubscribe user {} from group {}; the feed was left unchanged. \
                         Removing another owner's roles needs USER_ADMIN (or foundation \
                         membership) on the payer in addition to FEED_AUTHORITY: doublezero \
                         permission set --user-payer <payer> --add user-admin",
                        orphan.user_pk, orphan.group_pk
                    )
                })?;
            writeln!(
                out,
                "  unsubscribed user {} from group {}",
                orphan.user_pk, orphan.group_pk
            )?;
        }
        rounds_done += 1;
    }
}

/// Reads every account class the planner needs. Users and access passes carry the memberships and
/// seats; devices resolve a user's metro; feeds resolve what the pass's other seats still cover.
fn scan<C: CliCommand>(client: &C) -> eyre::Result<Snapshot> {
    Ok(Snapshot {
        users: client.list_user(ListUserCommand {})?,
        accesspasses: client.list_accesspass(ListAccessPassCommand {})?,
        devices: client.list_device(ListDeviceCommand {})?,
        feeds: client.list_feed(ListFeedCommand {})?,
    })
}

/// The memberships that dropping `dropped` from `feed_pk` would orphan.
///
/// A membership survives when some *other* feed seated on the same access pass still carries the
/// group and serves the user's metro — that is the same coverage test the program applies at
/// connect time. The rotated feed itself never counts: every group in `dropped` is by construction
/// absent from its post-change set, and a delete removes it entirely.
pub fn plan(feed_pk: &Pubkey, dropped: &[Pubkey], snap: &Snapshot) -> eyre::Result<Vec<Orphan>> {
    let mut orphans = Vec::new();

    for (user_pk, user) in &snap.users {
        if user.publishers.is_empty() && user.subscribers.is_empty() {
            continue;
        }
        // Non-EdgeSeat memberships come from the pass's own allowlists, not from feeds, so feed
        // changes cannot orphan them.
        let Some(pass) = resolve_pass(&snap.accesspasses, user) else {
            continue;
        };
        if !matches!(pass.accesspass_type, AccessPassType::EdgeSeat(_)) {
            continue;
        }

        // Without the device we cannot tell which metro the user connects from, and therefore
        // cannot tell coverage from non-coverage. Refuse rather than guess in either direction.
        let device = snap.devices.get(&user.device_pk).ok_or_else(|| {
            eyre::eyre!(
                "user {user_pk} references unknown device {}; cannot evaluate feed coverage",
                user.device_pk
            )
        })?;

        for group_pk in dropped {
            let publisher = user.publishers.contains(group_pk);
            let subscriber = user.subscribers.contains(group_pk);
            if !publisher && !subscriber {
                continue;
            }
            let covered = pass.feed_seats().iter().any(|seat| {
                seat.feed_key != *feed_pk
                    && snap.feeds.get(&seat.feed_key).is_some_and(|other| {
                        other.groups_for(&device.exchange_pk).contains(group_pk)
                    })
            });
            if !covered {
                orphans.push(Orphan {
                    user_pk: *user_pk,
                    group_pk: *group_pk,
                    client_ip: user.client_ip,
                    publisher,
                    subscriber,
                });
            }
        }
    }

    // The scan walks HashMaps, so sort for a stable plan and stable output.
    orphans.sort();
    Ok(orphans)
}

/// The access pass serviceability would resolve for `user`, mirroring `GetAccessPassCommand`: a
/// shared dynamic pass at the UNSPECIFIED-IP PDA wins over the exact-IP pass.
fn resolve_pass<'a>(
    accesspasses: &'a HashMap<Pubkey, AccessPass>,
    user: &User,
) -> Option<&'a AccessPass> {
    let mut exact = None;
    for pass in accesspasses.values() {
        if pass.user_payer != user.owner {
            continue;
        }
        if pass.client_ip == Ipv4Addr::UNSPECIFIED {
            return Some(pass);
        }
        if pass.client_ip == user.client_ip {
            exact = Some(pass);
        }
    }
    exact
}

fn report<W: Write>(
    out: &mut W,
    feed_code: &str,
    orphans: &[Orphan],
    rounds_done: usize,
) -> eyre::Result<()> {
    if rounds_done == 0 {
        writeln!(
            out,
            "Changing feed '{feed_code}' orphans {} multicast subscription(s):",
            orphans.len()
        )?;
    } else {
        writeln!(
            out,
            "{} new orphaned subscription(s) appeared on feed '{feed_code}' during the previous \
             round:",
            orphans.len()
        )?;
    }
    for orphan in orphans {
        let mut roles = Vec::new();
        if orphan.publisher {
            roles.push("publisher");
        }
        if orphan.subscriber {
            roles.push("subscriber");
        }
        writeln!(
            out,
            "  user {} ({}) group {} [{}]",
            orphan.user_pk,
            orphan.client_ip,
            orphan.group_pk,
            roles.join(", ")
        )?;
    }
    Ok(())
}

/// Account builders shared by the guard's planner tests and the `feed update` / `feed delete`
/// command tests, which stub the same four scans through the mock client.
#[cfg(test)]
pub(crate) mod fixtures {
    use super::*;
    use doublezero_sdk::{AccountType, DeviceStatus, UserCYOA, UserStatus, UserType};
    use doublezero_serviceability::state::accesspass::{AccessPassStatus, FeedSeat};

    pub fn device(exchange_pk: Pubkey) -> Device {
        Device {
            account_type: AccountType::Device,
            exchange_pk,
            status: DeviceStatus::Activated,
            ..Default::default()
        }
    }

    pub fn feed(exchange: Pubkey, groups: Vec<Pubkey>) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            code: "feed".to_string(),
            name: "Feed".to_string(),
            exchange,
            groups,
        }
    }

    pub fn seat(feed_key: Pubkey) -> FeedSeat {
        FeedSeat {
            feed_key,
            max_users: 1,
            max_future_users: 1,
            current_users: 1,
            anniversary_day: 1,
            window_end: 0,
            terminates_at: 0,
        }
    }

    pub fn pass(
        user_payer: Pubkey,
        client_ip: Ipv4Addr,
        accesspass_type: AccessPassType,
    ) -> AccessPass {
        AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            accesspass_type,
            client_ip,
            user_payer,
            last_access_epoch: u64::MAX,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
            tenant_allowlist: vec![],
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 1,
            max_multicast_users: 1,
        }
    }

    pub fn user(
        owner: Pubkey,
        device_pk: Pubkey,
        client_ip: Ipv4Addr,
        subscribers: Vec<Pubkey>,
    ) -> User {
        User {
            account_type: AccountType::User,
            owner,
            index: 1,
            bump_seed: 255,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::default(),
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: [10, 0, 0, 1].into(),
            tunnel_id: 1,
            tunnel_net: "10.0.0.0/31".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers,
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{fixtures::*, *};
    use doublezero_serviceability::state::accesspass::AccessPassType;

    /// One EdgeSeat user subscribed to `group`, seated on `feed_pk` in `exchange`.
    struct Fixture {
        exchange: Pubkey,
        feed_pk: Pubkey,
        group: Pubkey,
        user_pk: Pubkey,
        client_ip: Ipv4Addr,
        snap: Snapshot,
    }

    fn fixture() -> Fixture {
        let exchange = Pubkey::new_unique();
        let feed_pk = Pubkey::new_unique();
        let group = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let client_ip: Ipv4Addr = [10, 1, 1, 1].into();

        Fixture {
            exchange,
            feed_pk,
            group,
            user_pk,
            client_ip,
            snap: Snapshot {
                users: HashMap::from([(user_pk, user(owner, device_pk, client_ip, vec![group]))]),
                accesspasses: HashMap::from([(
                    Pubkey::new_unique(),
                    pass(
                        owner,
                        client_ip,
                        AccessPassType::EdgeSeat(vec![seat(feed_pk)]),
                    ),
                )]),
                devices: HashMap::from([(device_pk, device(exchange))]),
                feeds: HashMap::from([(feed_pk, feed(exchange, vec![group]))]),
            },
        }
    }

    #[test]
    fn test_plan_flags_dropped_group_the_user_holds() {
        let f = fixture();
        let orphans = plan(&f.feed_pk, &[f.group], &f.snap).unwrap();
        assert_eq!(
            orphans,
            vec![Orphan {
                user_pk: f.user_pk,
                group_pk: f.group,
                client_ip: f.client_ip,
                publisher: false,
                subscriber: true,
            }]
        );
    }

    #[test]
    fn test_plan_ignores_dropped_group_the_user_does_not_hold() {
        let f = fixture();
        assert!(plan(&f.feed_pk, &[Pubkey::new_unique()], &f.snap)
            .unwrap()
            .is_empty());
    }

    #[test]
    fn test_plan_empty_when_nothing_dropped() {
        let f = fixture();
        assert!(plan(&f.feed_pk, &[], &f.snap).unwrap().is_empty());
    }

    #[test]
    fn test_plan_skips_group_covered_by_another_seated_feed_in_the_same_metro() {
        let mut f = fixture();
        let other_pk = Pubkey::new_unique();
        f.snap
            .feeds
            .insert(other_pk, feed(f.exchange, vec![f.group]));
        let owner = f.snap.users[&f.user_pk].owner;
        f.snap.accesspasses = HashMap::from([(
            Pubkey::new_unique(),
            pass(
                owner,
                f.client_ip,
                AccessPassType::EdgeSeat(vec![seat(f.feed_pk), seat(other_pk)]),
            ),
        )]);

        assert!(plan(&f.feed_pk, &[f.group], &f.snap).unwrap().is_empty());
    }

    #[test]
    fn test_plan_does_not_count_another_feed_in_a_different_metro() {
        let mut f = fixture();
        let other_pk = Pubkey::new_unique();
        f.snap
            .feeds
            .insert(other_pk, feed(Pubkey::new_unique(), vec![f.group]));
        let owner = f.snap.users[&f.user_pk].owner;
        f.snap.accesspasses = HashMap::from([(
            Pubkey::new_unique(),
            pass(
                owner,
                f.client_ip,
                AccessPassType::EdgeSeat(vec![seat(f.feed_pk), seat(other_pk)]),
            ),
        )]);

        assert_eq!(plan(&f.feed_pk, &[f.group], &f.snap).unwrap().len(), 1);
    }

    /// The sharp edge: the rotated feed's *old* set is not coverage. Its own seat must never
    /// excuse a group it is about to stop carrying.
    #[test]
    fn test_plan_does_not_count_the_rotated_feeds_own_seat_as_coverage() {
        let f = fixture();
        // `feeds` still holds the pre-rotation group set, and the pass is seated on it.
        assert!(f.snap.feeds[&f.feed_pk].groups.contains(&f.group));
        assert_eq!(plan(&f.feed_pk, &[f.group], &f.snap).unwrap().len(), 1);
    }

    #[test]
    fn test_plan_covers_delete_by_dropping_every_group() {
        let mut f = fixture();
        let second = Pubkey::new_unique();
        f.snap
            .feeds
            .insert(f.feed_pk, feed(f.exchange, vec![f.group, second]));
        f.snap
            .users
            .get_mut(&f.user_pk)
            .unwrap()
            .subscribers
            .push(second);

        let groups = f.snap.feeds[&f.feed_pk].groups.clone();
        let orphans = plan(&f.feed_pk, &groups, &f.snap).unwrap();
        assert_eq!(orphans.len(), 2);
        assert!(orphans.iter().all(|o| o.user_pk == f.user_pk));
    }

    #[test]
    fn test_plan_flags_the_publisher_role() {
        let mut f = fixture();
        let u = f.snap.users.get_mut(&f.user_pk).unwrap();
        u.subscribers.clear();
        u.publishers.push(f.group);

        let orphans = plan(&f.feed_pk, &[f.group], &f.snap).unwrap();
        assert_eq!(orphans.len(), 1);
        assert!(orphans[0].publisher && !orphans[0].subscriber);
    }

    #[test]
    fn test_plan_leaves_non_edgeseat_users_alone() {
        let mut f = fixture();
        let owner = f.snap.users[&f.user_pk].owner;
        f.snap.accesspasses = HashMap::from([(
            Pubkey::new_unique(),
            pass(owner, f.client_ip, AccessPassType::Prepaid),
        )]);

        assert!(plan(&f.feed_pk, &[f.group], &f.snap).unwrap().is_empty());
    }

    #[test]
    fn test_plan_prefers_the_shared_dynamic_pass() {
        let mut f = fixture();
        let owner = f.snap.users[&f.user_pk].owner;
        // The exact-IP pass is EdgeSeat, but the shared dynamic pass is what the program reads —
        // and it is Prepaid, so this user is not feed-gated.
        f.snap.accesspasses.insert(
            Pubkey::new_unique(),
            pass(owner, Ipv4Addr::UNSPECIFIED, AccessPassType::Prepaid),
        );

        assert!(plan(&f.feed_pk, &[f.group], &f.snap).unwrap().is_empty());
    }

    #[test]
    fn test_plan_errors_on_a_user_with_an_unknown_device() {
        let mut f = fixture();
        f.snap.devices.clear();
        let err = plan(&f.feed_pk, &[f.group], &f.snap).unwrap_err();
        assert!(
            err.to_string().contains("unknown device"),
            "unexpected error: {err}"
        );
    }
}
