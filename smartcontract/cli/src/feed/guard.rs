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
        accesspass::list::ListAccessPassCommand,
        device::list::ListDeviceCommand,
        feed::list::ListFeedCommand,
        multicastgroup::subscribe::{UpdateMulticastGroupRolesCommand, MAX_GROUPS_PER_TRANSACTION},
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
///
/// Roles are tracked separately because coverage is per role: the pass's own
/// `mgroup_pub_allowlist` / `mgroup_sub_allowlist` authorize a role with no feed involved, so one
/// role of a group can be orphaned while the other legitimately stays.
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
pub struct Orphan {
    pub user_pk: Pubkey,
    pub group_pk: Pubkey,
    pub client_ip: Ipv4Addr,
    /// The user holds the publisher role and nothing covers it. EdgeSeat is meant to be
    /// subscribe-only, so this is worth calling out in the plan an operator reads before forcing
    /// the change through.
    pub remove_publisher: bool,
    pub remove_subscriber: bool,
    /// Held roles an accepted pass's own allowlist still authorizes. These cannot be preserved
    /// through the automated removal — `UpdateMulticastGroupRoles` reads a `true` as a grant, not
    /// "leave it alone", which flips the required permission to ACCESS_PASS_ADMIN, requires the
    /// user be Activated, and re-checks the allowlist against the single pass the SDK resolves —
    /// so an orphan carrying one of these fails the run closed for manual handling.
    pub keep_publisher: bool,
    pub keep_subscriber: bool,
}

impl Orphan {
    /// One role must go while the other is still authorized — not expressible as the
    /// removal-only update the automated path issues.
    fn is_mixed(&self) -> bool {
        self.keep_publisher || self.keep_subscriber
    }
}

/// The onchain state [`find_orphans`] reads, fetched once per round by [`scan`].
pub struct Snapshot {
    pub users: HashMap<Pubkey, User>,
    pub accesspasses: HashMap<Pubkey, AccessPass>,
    pub devices: HashMap<Pubkey, Device>,
    pub feeds: HashMap<Pubkey, Feed>,
}

/// Unsubscribes every EdgeSeat user that replacing `feed_pk`'s group set with `new_groups` would
/// orphan, leaving the caller to submit the feed change afterwards. A delete passes an empty
/// `new_groups`, since it drops everything the feed carried.
///
/// Without `force` this only reports: a non-empty plan fails the command before anything is
/// submitted. With `force` the removals are applied and re-verified against a fresh scan.
/// Removals are idempotent, so a run that fails partway is safe to repeat.
pub fn unsubscribe_orphans<C: CliCommand, W: Write>(
    client: &C,
    out: &mut W,
    feed_pk: &Pubkey,
    feed_code: &str,
    new_groups: &[Pubkey],
    force: bool,
) -> eyre::Result<()> {
    let mut rounds_done = 0;
    loop {
        let snap = scan(client)?;

        // Re-derive the dropped set from the freshly scanned feed rather than from the group set
        // the command read before the loop: a group added to the feed mid-run is dropped by this
        // change too, and pinning the earlier set would let its subscribers through unseen.
        let feed = snap.feeds.get(feed_pk).ok_or_else(|| {
            eyre::eyre!(
                "feed '{feed_code}' ({feed_pk}) disappeared mid-run; the feed change was \
                 not submitted"
            )
        })?;
        let dropped: Vec<Pubkey> = feed
            .groups
            .iter()
            .filter(|g| !new_groups.contains(g))
            .copied()
            .collect();

        let orphans = find_orphans(feed_pk, &dropped, &snap)?;
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
        if orphans.iter().any(Orphan::is_mixed) {
            eyre::bail!(
                "feed '{feed_code}' has membership(s) where one role must be removed while the \
                 other is still authorized by the pass allowlist (flagged above). The automated \
                 path only issues removal-only updates, so clean those users up manually, then \
                 re-run. Nothing was submitted this round."
            );
        }

        // Every orphan reaching this point is removal-only (mixed roles bailed
        // above), so a user's removals batch into one atomic role update, chunked
        // to the transaction limit, instead of one transaction per group. The plan
        // is sorted by (user, group), so one linear pass groups it.
        let mut batches: Vec<(&Orphan, Vec<Pubkey>)> = Vec::new();
        for orphan in &orphans {
            match batches.last_mut() {
                Some((first, group_pks)) if first.user_pk == orphan.user_pk => {
                    group_pks.push(orphan.group_pk)
                }
                _ => batches.push((orphan, vec![orphan.group_pk])),
            }
        }
        for (orphan, group_pks) in batches {
            for chunk in group_pks.chunks(MAX_GROUPS_PER_TRANSACTION) {
                let groups = chunk
                    .iter()
                    .map(ToString::to_string)
                    .collect::<Vec<_>>()
                    .join(", ");
                client
                    .update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                        user_pk: orphan.user_pk,
                        group_pks: chunk.to_vec(),
                        client_ip: orphan.client_ip,
                        publisher: false,
                        subscriber: false,
                        device_pk: None,
                        feed_pk: None,
                    })
                    .wrap_err_with(|| {
                        format!(
                            "failed to unsubscribe user {} from group(s) {groups}; the feed was \
                             left unchanged. If this is an authorization failure, removing another \
                             owner's roles needs USER_ADMIN (or foundation membership) on the payer \
                             in addition to FEED_AUTHORITY: doublezero permission set --user-payer \
                             <payer> --add user-admin",
                            orphan.user_pk
                        )
                    })?;
                writeln!(
                    out,
                    "  unsubscribed user {} from group(s) {groups}",
                    orphan.user_pk
                )?;
            }
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
/// A role survives when another feed the user holds a consumed seat on (`user.feed_pks`, the set
/// seat release retains against) still carries the group in their metro, or when the pass's own
/// per-role allowlist authorizes it. The rotated feed itself never counts: every group in
/// `dropped` is by construction absent from its post-change set.
pub fn find_orphans(
    feed_pk: &Pubkey,
    dropped: &[Pubkey],
    snap: &Snapshot,
) -> eyre::Result<Vec<Orphan>> {
    let mut orphans = Vec::new();

    // Index once so the per-user pass lookup doesn't rescan every pass (O(users × passes) when a
    // drop touches many users).
    let mut passes_by_payer: HashMap<Pubkey, Vec<&AccessPass>> = HashMap::new();
    for pass in snap.accesspasses.values() {
        passes_by_payer
            .entry(pass.user_payer)
            .or_default()
            .push(pass);
    }

    for (user_pk, user) in &snap.users {
        let held: Vec<&Pubkey> = dropped
            .iter()
            .filter(|g| user.publishers.contains(g) || user.subscribers.contains(g))
            .collect();
        if held.is_empty() {
            continue;
        }

        // Every pass the program would accept for this user.
        let passes: Vec<&AccessPass> = passes_by_payer
            .get(&user.owner)
            .into_iter()
            .flatten()
            .filter(|pass| {
                pass.client_ip == Ipv4Addr::UNSPECIFIED || pass.client_ip == user.client_ip
            })
            .copied()
            .collect();
        let seated: Vec<Pubkey> = passes
            .iter()
            .flat_map(|pass| pass.feed_seats().iter().map(|seat| seat.feed_key))
            .collect();
        // Not feed-gated: with no EdgeSeat pass, memberships come from the pass's own allowlists,
        // so no feed change can orphan them and no seat is at stake. `feed_pks` is the record of
        // seats actually consumed at connect, so a user holding one stays in scope even if their
        // pass no longer shows the seat.
        let edge_seat = passes
            .iter()
            .any(|pass| matches!(pass.accesspass_type, AccessPassType::EdgeSeat(_)));
        if !edge_seat && user.feed_pks.is_empty() {
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

        for group_pk in held {
            // Feed coverage is role-agnostic; allowlist coverage is per role. Only a seat the
            // user consumed counts, mirroring the retained-feeds set seat release checks.
            let feed_covered = seated.iter().any(|seated_pk| {
                seated_pk != feed_pk
                    && user.feed_pks.contains(seated_pk)
                    && snap.feeds.get(seated_pk).is_some_and(|other| {
                        other.groups_for(&device.exchange_pk).contains(group_pk)
                    })
            });
            let pub_covered = feed_covered
                || passes
                    .iter()
                    .any(|pass| pass.mgroup_pub_allowlist.contains(group_pk));
            let sub_covered = feed_covered
                || passes
                    .iter()
                    .any(|pass| pass.mgroup_sub_allowlist.contains(group_pk));

            let pub_held = user.publishers.contains(group_pk);
            let sub_held = user.subscribers.contains(group_pk);
            let remove_publisher = pub_held && !pub_covered;
            let remove_subscriber = sub_held && !sub_covered;
            if remove_publisher || remove_subscriber {
                orphans.push(Orphan {
                    user_pk: *user_pk,
                    group_pk: *group_pk,
                    client_ip: user.client_ip,
                    remove_publisher,
                    remove_subscriber,
                    keep_publisher: pub_held && pub_covered,
                    keep_subscriber: sub_held && sub_covered,
                });
            }
        }
    }

    // The scan walks HashMaps, so sort for a stable plan and stable output.
    orphans.sort();
    Ok(orphans)
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
        if orphan.remove_publisher {
            roles.push("publisher");
        }
        if orphan.remove_subscriber {
            roles.push("subscriber");
        }
        let mut kept = Vec::new();
        if orphan.keep_publisher {
            kept.push("publisher");
        }
        if orphan.keep_subscriber {
            kept.push("subscriber");
        }
        writeln!(
            out,
            "  user {} ({}) group {} [{}]{}",
            orphan.user_pk,
            orphan.client_ip,
            orphan.group_pk,
            roles.join(", "),
            if kept.is_empty() {
                String::new()
            } else {
                format!(
                    " ({} still allowlisted — resolve manually)",
                    kept.join(", ")
                )
            }
        )?;
    }
    Ok(())
}

/// Account builders shared by the guard's planner tests and the `feed update` / `feed delete`
/// command tests, which stub the same four scans through the mock client.
#[cfg(test)]
pub(crate) mod fixtures {
    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use doublezero_sdk::{
        commands::{feed::get::GetFeedCommand, multicastgroup::get::GetMulticastGroupCommand},
        AccountType, DeviceStatus, MulticastGroup, MulticastGroupStatus, UserCYOA, UserStatus,
        UserType,
    };
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
            ..Default::default()
        }
    }

    pub fn mgroup(code: &str) -> MulticastGroup {
        MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 255,
            tenant_pk: Pubkey::default(),
            code: code.to_string(),
            multicast_ip: [239, 1, 1, 1].into(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            owner: Pubkey::new_unique(),
            publisher_count: 0,
            subscriber_count: 0,
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

    /// A feed carrying `groups` and one EdgeSeat user in the feed's exchange, seated on it.
    /// Shared by the `feed update` / `feed delete` command tests, which stub the same guard
    /// scans through the mock client; `new(group_count)` picks how many groups the feed carries.
    pub struct GuardFixture {
        pub exchange_pk: Pubkey,
        pub feed_pk: Pubkey,
        pub groups: Vec<Pubkey>,
        pub device_pk: Pubkey,
        pub user_pk: Pubkey,
        pub owner: Pubkey,
        pub client_ip: Ipv4Addr,
    }

    impl GuardFixture {
        pub fn new(group_count: usize) -> Self {
            Self {
                exchange_pk: Pubkey::new_unique(),
                feed_pk: Pubkey::new_unique(),
                groups: (0..group_count).map(|_| Pubkey::new_unique()).collect(),
                device_pk: Pubkey::new_unique(),
                user_pk: Pubkey::new_unique(),
                owner: Pubkey::new_unique(),
                client_ip: [10, 1, 1, 1].into(),
            }
        }

        /// Stub `get_feed` for a `--pubkey <feed_pk>` lookup, returning a feed carrying `groups`.
        pub fn expect_get_feed(&self, client: &mut MockCliCommand, groups: Vec<Pubkey>) {
            let feed_pk = self.feed_pk;
            let feed_acct = feed(self.exchange_pk, groups);
            client
                .expect_get_feed()
                .with(mockall::predicate::eq(GetFeedCommand {
                    pubkey_or_code: feed_pk.to_string(),
                    exchange: None,
                }))
                .times(1)
                .returning(move |_| Ok((feed_pk, feed_acct.clone())));
        }

        /// Stub `get_multicastgroup` for every group the fixture carries, keyed by pubkey, so a
        /// `--group <pubkey>` argument resolves.
        pub fn expect_get_groups(&self, client: &mut MockCliCommand) {
            for (i, group_pk) in self.groups.iter().copied().enumerate() {
                let group_acct = mgroup(&format!("mg{i}"));
                client
                    .expect_get_multicastgroup()
                    .with(mockall::predicate::eq(GetMulticastGroupCommand {
                        pubkey_or_code: group_pk.to_string(),
                    }))
                    .returning(move |_| Ok((group_pk, group_acct.clone())));
            }
        }

        /// Stub one guard scan whose user holds `subscribers`. Stacked expectations match FIFO,
        /// so calling this twice lets a second scan see a different snapshot.
        pub fn expect_scan(&self, client: &mut MockCliCommand, subscribers: Vec<Pubkey>) {
            let user_pk = self.user_pk;
            let user_acct = user(self.owner, self.device_pk, self.client_ip, subscribers);
            client
                .expect_list_user()
                .times(1)
                .returning(move |_| Ok(HashMap::from([(user_pk, user_acct.clone())])));
            let pass_acct = pass(
                self.owner,
                self.client_ip,
                AccessPassType::EdgeSeat(vec![seat(self.feed_pk)]),
            );
            client
                .expect_list_accesspass()
                .times(1)
                .returning(move |_| Ok(HashMap::from([(Pubkey::new_unique(), pass_acct.clone())])));
            let device_pk = self.device_pk;
            let device_acct = device(self.exchange_pk);
            client
                .expect_list_device()
                .times(1)
                .returning(move |_| Ok(HashMap::from([(device_pk, device_acct.clone())])));
            let feed_pk = self.feed_pk;
            let feed_acct = feed(self.exchange_pk, self.groups.clone());
            client
                .expect_list_feed()
                .times(1)
                .returning(move |_| Ok(HashMap::from([(feed_pk, feed_acct.clone())])));
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
        let orphans = find_orphans(&f.feed_pk, &[f.group], &f.snap).unwrap();
        assert_eq!(
            orphans,
            vec![Orphan {
                user_pk: f.user_pk,
                group_pk: f.group,
                client_ip: f.client_ip,
                remove_publisher: false,
                remove_subscriber: true,
                keep_publisher: false,
                keep_subscriber: false,
            }]
        );
    }

    #[test]
    fn test_plan_ignores_dropped_group_the_user_does_not_hold() {
        let f = fixture();
        assert!(find_orphans(&f.feed_pk, &[Pubkey::new_unique()], &f.snap)
            .unwrap()
            .is_empty());
    }

    #[test]
    fn test_plan_empty_when_nothing_dropped() {
        let f = fixture();
        assert!(find_orphans(&f.feed_pk, &[], &f.snap).unwrap().is_empty());
    }

    #[test]
    fn test_plan_skips_group_covered_by_another_consumed_feed_in_the_same_metro() {
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
        f.snap.users.get_mut(&f.user_pk).unwrap().feed_pks = vec![f.feed_pk, other_pk];

        assert!(find_orphans(&f.feed_pk, &[f.group], &f.snap)
            .unwrap()
            .is_empty());
    }

    /// A seated feed whose seat the user never consumed (absent from `feed_pks`) is not coverage:
    /// seat release only retains against consumed seats, so the membership would outlive its
    /// accounting.
    #[test]
    fn test_plan_does_not_count_a_seated_feed_the_user_never_consumed() {
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
        f.snap.users.get_mut(&f.user_pk).unwrap().feed_pks = vec![f.feed_pk];

        assert_eq!(
            find_orphans(&f.feed_pk, &[f.group], &f.snap).unwrap().len(),
            1
        );
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
        // Consumed, so the metro mismatch is the only reason it doesn't cover.
        f.snap.users.get_mut(&f.user_pk).unwrap().feed_pks = vec![f.feed_pk, other_pk];

        assert_eq!(
            find_orphans(&f.feed_pk, &[f.group], &f.snap).unwrap().len(),
            1
        );
    }

    /// The sharp edge: the rotated feed's *old* set is not coverage. Its own seat must never
    /// excuse a group it is about to stop carrying.
    #[test]
    fn test_plan_does_not_count_the_rotated_feeds_own_seat_as_coverage() {
        let mut f = fixture();
        // `feeds` still holds the pre-rotation group set, the pass is seated on it, and the seat
        // is even consumed — none of which excuses a group the feed is about to stop carrying.
        f.snap.users.get_mut(&f.user_pk).unwrap().feed_pks = vec![f.feed_pk];
        assert!(f.snap.feeds[&f.feed_pk].groups.contains(&f.group));
        assert_eq!(
            find_orphans(&f.feed_pk, &[f.group], &f.snap).unwrap().len(),
            1
        );
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
        let orphans = find_orphans(&f.feed_pk, &groups, &f.snap).unwrap();
        assert_eq!(orphans.len(), 2);
        assert!(orphans.iter().all(|o| o.user_pk == f.user_pk));
    }

    #[test]
    fn test_plan_flags_the_publisher_role() {
        let mut f = fixture();
        let u = f.snap.users.get_mut(&f.user_pk).unwrap();
        u.subscribers.clear();
        u.publishers.push(f.group);

        let orphans = find_orphans(&f.feed_pk, &[f.group], &f.snap).unwrap();
        assert_eq!(orphans.len(), 1);
        assert!(orphans[0].remove_publisher && !orphans[0].remove_subscriber);
    }

    #[test]
    fn test_plan_leaves_non_edgeseat_users_alone() {
        let mut f = fixture();
        let owner = f.snap.users[&f.user_pk].owner;
        f.snap.accesspasses = HashMap::from([(
            Pubkey::new_unique(),
            pass(owner, f.client_ip, AccessPassType::Prepaid),
        )]);

        assert!(find_orphans(&f.feed_pk, &[f.group], &f.snap)
            .unwrap()
            .is_empty());
    }

    /// The program accepts a pass at either PDA, so a shared dynamic pass must not hide the
    /// EdgeSeat pass at the exact-IP PDA the user is actually seated on.
    #[test]
    fn test_plan_sees_an_edgeseat_pass_shadowed_by_a_dynamic_prepaid_pass() {
        let mut f = fixture();
        let owner = f.snap.users[&f.user_pk].owner;
        f.snap.accesspasses.insert(
            Pubkey::new_unique(),
            pass(owner, Ipv4Addr::UNSPECIFIED, AccessPassType::Prepaid),
        );

        assert_eq!(
            find_orphans(&f.feed_pk, &[f.group], &f.snap).unwrap().len(),
            1
        );
    }

    /// Coverage is the union over both accepted PDAs, not just one of them.
    #[test]
    fn test_plan_counts_coverage_from_a_pass_at_the_other_pda() {
        let mut f = fixture();
        let owner = f.snap.users[&f.user_pk].owner;
        let other_pk = Pubkey::new_unique();
        f.snap
            .feeds
            .insert(other_pk, feed(f.exchange, vec![f.group]));
        f.snap.accesspasses.insert(
            Pubkey::new_unique(),
            pass(
                owner,
                Ipv4Addr::UNSPECIFIED,
                AccessPassType::EdgeSeat(vec![seat(other_pk)]),
            ),
        );
        f.snap.users.get_mut(&f.user_pk).unwrap().feed_pks = vec![f.feed_pk, other_pk];

        assert!(find_orphans(&f.feed_pk, &[f.group], &f.snap)
            .unwrap()
            .is_empty());
    }

    /// A consumed seat keeps the user in scope even if their pass no longer shows it.
    #[test]
    fn test_plan_keeps_a_user_with_a_consumed_seat_but_no_edgeseat_pass() {
        let mut f = fixture();
        let owner = f.snap.users[&f.user_pk].owner;
        f.snap.accesspasses = HashMap::from([(
            Pubkey::new_unique(),
            pass(owner, f.client_ip, AccessPassType::Prepaid),
        )]);
        f.snap.users.get_mut(&f.user_pk).unwrap().feed_pks = vec![f.feed_pk];

        assert_eq!(
            find_orphans(&f.feed_pk, &[f.group], &f.snap).unwrap().len(),
            1
        );
    }

    #[test]
    fn test_plan_errors_on_a_user_with_an_unknown_device() {
        let mut f = fixture();
        f.snap.devices.clear();
        let err = find_orphans(&f.feed_pk, &[f.group], &f.snap).unwrap_err();
        assert!(
            err.to_string().contains("unknown device"),
            "unexpected error: {err}"
        );
    }

    /// A dangling `device_pk` on a user who holds none of the dropped groups must not block the
    /// rotation — the metro is only needed to judge coverage for a group actually held.
    #[test]
    fn test_plan_ignores_an_unknown_device_on_an_unaffected_user() {
        let mut f = fixture();
        let stray_pk = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        f.snap.users.insert(
            stray_pk,
            user(
                owner,
                Pubkey::new_unique(),
                [10, 2, 2, 2].into(),
                vec![Pubkey::new_unique()],
            ),
        );
        f.snap.accesspasses.insert(
            Pubkey::new_unique(),
            pass(
                owner,
                [10, 2, 2, 2].into(),
                AccessPassType::EdgeSeat(vec![seat(f.feed_pk)]),
            ),
        );

        assert_eq!(
            find_orphans(&f.feed_pk, &[f.group], &f.snap).unwrap().len(),
            1
        );
    }

    /// The program authorizes a role through the pass's own allowlist for every pass type, so a
    /// subscriber the sub allowlist still covers is not orphaned by the feed change.
    #[test]
    fn test_plan_skips_a_role_covered_by_the_pass_sub_allowlist() {
        let mut f = fixture();
        let (_, pass) = f.snap.accesspasses.iter_mut().next().unwrap();
        pass.mgroup_sub_allowlist.push(f.group);

        assert!(find_orphans(&f.feed_pk, &[f.group], &f.snap)
            .unwrap()
            .is_empty());
    }

    /// Allowlist coverage is per role: a pub allowlist entry says nothing about the subscriber
    /// role.
    #[test]
    fn test_plan_pub_allowlist_does_not_cover_the_subscriber_role() {
        let mut f = fixture();
        let (_, pass) = f.snap.accesspasses.iter_mut().next().unwrap();
        pass.mgroup_pub_allowlist.push(f.group);

        assert_eq!(
            find_orphans(&f.feed_pk, &[f.group], &f.snap).unwrap().len(),
            1
        );
    }

    /// Mixed roles: the uncovered publisher role must go while the allowlist still authorizes the
    /// subscriber role — flagged as mixed so the run fails closed for manual handling.
    #[test]
    fn test_plan_flags_a_mixed_role_orphan() {
        let mut f = fixture();
        f.snap
            .users
            .get_mut(&f.user_pk)
            .unwrap()
            .publishers
            .push(f.group);
        let (_, pass) = f.snap.accesspasses.iter_mut().next().unwrap();
        pass.mgroup_sub_allowlist.push(f.group);

        let orphans = find_orphans(&f.feed_pk, &[f.group], &f.snap).unwrap();
        assert_eq!(
            orphans,
            vec![Orphan {
                user_pk: f.user_pk,
                group_pk: f.group,
                client_ip: f.client_ip,
                remove_publisher: true,
                remove_subscriber: false,
                keep_publisher: false,
                keep_subscriber: true,
            }]
        );
        assert!(orphans[0].is_mixed());
    }

    /// Allowlist coverage counts from any accepted pass, not just the one carrying the seat.
    #[test]
    fn test_plan_counts_allowlist_coverage_from_the_other_pda() {
        let mut f = fixture();
        let owner = f.snap.users[&f.user_pk].owner;
        let mut dynamic = pass(owner, Ipv4Addr::UNSPECIFIED, AccessPassType::Prepaid);
        dynamic.mgroup_sub_allowlist.push(f.group);
        f.snap.accesspasses.insert(Pubkey::new_unique(), dynamic);

        assert!(find_orphans(&f.feed_pk, &[f.group], &f.snap)
            .unwrap()
            .is_empty());
    }
}
