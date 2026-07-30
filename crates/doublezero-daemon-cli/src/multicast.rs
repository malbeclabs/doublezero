//! `doublezero multicast subscribe|unsubscribe|publish|unpublish` — daemon
//! transport verbs that mutate the operator's multicast user roles onchain.
//!
//! These commands are intentionally NOT `DaemonCommand` variants: hoisting them
//! would surface them as `doublezero subscribe`, breaking the existing
//! `doublezero multicast <verb>` invocation. The binary's `MulticastCommands`
//! enum nests them and routes here. Onchain multicast group CRUD
//! (`group list/create/...`) dispatches to the serviceability module crate,
//! not this one.
//!
//! Progress animation is rendered on a stderr spinner (transient UI);
//! informational and result lines route through the shared writer.

use std::{io::Write, net::Ipv4Addr};

use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::{
    commands::multicastgroup::subscribe::UpdateMulticastGroupRolesCommand, User, UserType,
};
use indicatif::ProgressBar;
use solana_sdk::pubkey::Pubkey;

use crate::{
    client::DaemonClient,
    helpers::{init_spinner, resolve_client_ip},
    ledger::LedgerClient,
};

/// Subscribe to one or more multicast groups (user must already be connected)
#[derive(Args, Debug)]
pub struct Subscribe {
    /// Multicast group code(s) to subscribe to
    #[arg(num_args = 1..)]
    pub groups: Vec<String>,
}

/// Unsubscribe from one or more multicast groups
#[derive(Args, Debug)]
pub struct Unsubscribe {
    /// Multicast group code(s) to unsubscribe from
    #[arg(num_args = 1..)]
    pub groups: Vec<String>,
}

/// Publish to one or more multicast groups (user must already be connected)
#[derive(Args, Debug)]
pub struct Publish {
    /// Multicast group code(s) to publish to
    #[arg(num_args = 1..)]
    pub groups: Vec<String>,
}

/// Stop publishing to one or more multicast groups
#[derive(Args, Debug)]
pub struct Unpublish {
    /// Multicast group code(s) to stop publishing to
    #[arg(num_args = 1..)]
    pub groups: Vec<String>,
}

/// Resolve a list of multicast group codes to their onchain pubkeys.
/// Errors on any unknown code, with no onchain writes.
fn resolve_groups<L: LedgerClient>(
    ledger: &L,
    codes: &[String],
) -> eyre::Result<Vec<(String, Pubkey)>> {
    let mcast_groups = ledger.list_multicastgroup()?;
    let mut out = Vec::with_capacity(codes.len());
    for code in codes {
        let (pk, _) = mcast_groups
            .iter()
            .find(|(_, g)| g.code == *code)
            .ok_or_else(|| eyre::eyre!("Multicast group not found: {code}"))?;
        out.push((code.clone(), *pk));
    }
    Ok(out)
}

/// Load the Multicast user for the given client_ip. Errors if none exists.
fn load_multicast_user<L: LedgerClient>(
    ledger: &L,
    client_ip: Ipv4Addr,
) -> eyre::Result<(Pubkey, User)> {
    let users = ledger.list_user()?;
    users
        .into_iter()
        .find(|(_, u)| u.client_ip == client_ip && u.user_type == UserType::Multicast)
        .ok_or_else(|| {
            eyre::eyre!(
                "No active multicast user for {client_ip}. \
                 Run 'doublezero connect Multicast --publish/--subscribe <group>' first."
            )
        })
}

fn finish_update<W: Write>(spinner: &ProgressBar, out: &mut W) -> eyre::Result<()> {
    writeln!(out, "✅  Updated. Routes will adjust shortly.")?;
    spinner.finish_and_clear();
    Ok(())
}

/// A single batched role update: one atomic transaction applying the same
/// (publisher, subscriber) pair to every group in `groups`.
struct RoleUpdateBatch {
    publisher: bool,
    subscriber: bool,
    groups: Vec<(String, Pubkey)>,
}

/// Partition `(code, pk, (publisher, subscriber))` triples into batches sharing a
/// flag pair, preserving encounter order. The instruction applies one flag pair to
/// every group it carries, so each distinct pair needs its own transaction (at most
/// two per verb: the carried role is the only variable).
fn batch_role_updates(groups: Vec<(String, Pubkey, (bool, bool))>) -> Vec<RoleUpdateBatch> {
    let mut batches: Vec<RoleUpdateBatch> = Vec::new();
    for (code, pk, (publisher, subscriber)) in groups {
        match batches
            .iter_mut()
            .find(|b| (b.publisher, b.subscriber) == (publisher, subscriber))
        {
            Some(batch) => batch.groups.push((code, pk)),
            None => batches.push(RoleUpdateBatch {
                publisher,
                subscriber,
                groups: vec![(code, pk)],
            }),
        }
    }
    batches
}

/// Send each batch as one atomic transaction, printing a per-group `ok_verb` line on
/// success. A failed batch reports every code it carried (nothing in it was applied)
/// and its codes are returned as failures.
fn apply_role_update_batches<L: LedgerClient, W: Write>(
    ledger: &L,
    out: &mut W,
    user_pk: Pubkey,
    client_ip: Ipv4Addr,
    batches: Vec<RoleUpdateBatch>,
    ok_verb: &str,
    fail_verb: &str,
) -> eyre::Result<Vec<String>> {
    let mut failures: Vec<String> = Vec::new();
    for batch in batches {
        match ledger.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
            user_pk,
            group_pks: batch.groups.iter().map(|(_, pk)| *pk).collect(),
            client_ip,
            publisher: batch.publisher,
            subscriber: batch.subscriber,
            device_pk: None,
            feed_pk: None,
        }) {
            Ok(()) => {
                for (code, _) in &batch.groups {
                    writeln!(out, "    {ok_verb} {code}")?;
                }
            }
            Err(e) => {
                let codes: Vec<&str> = batch.groups.iter().map(|(c, _)| c.as_str()).collect();
                writeln!(out, "    ❌ failed to {fail_verb} {}: {e}", codes.join(", "))?;
                failures.extend(batch.groups.iter().map(|(c, _)| c.clone()));
            }
        }
    }
    Ok(failures)
}

/// If any batched calls failed, surface a non-zero exit by returning an error
/// listing the affected codes. Per-batch failures are already printed inline.
fn report_failures(op: &str, failures: &[String]) -> eyre::Result<()> {
    if failures.is_empty() {
        return Ok(());
    }
    Err(eyre::eyre!(
        "{op} failed for {} group(s): {}",
        failures.len(),
        failures.join(", ")
    ))
}

/// Returns true when removing `to_remove` publisher roles from `user` would leave
/// `user.publishers` empty (and the user currently has at least one publisher role).
fn would_empty_publishers(user: &User, to_remove: &[Pubkey]) -> bool {
    if user.publishers.is_empty() {
        return false;
    }
    let remaining = user
        .publishers
        .iter()
        .filter(|p| !to_remove.contains(p))
        .count();
    remaining == 0
}

/// Returns true when applying the given publisher + subscriber removals would
/// leave the user with zero multicast roles (i.e. an idle tunnel). Returns
/// false when the call is a complete no-op (nothing to remove) so an already-
/// empty user doesn't produce a spurious warning.
fn would_empty_all_roles(user: &User, remove_pubs: &[Pubkey], remove_subs: &[Pubkey]) -> bool {
    if remove_pubs.is_empty() && remove_subs.is_empty() {
        return false;
    }
    let remaining_pubs = user
        .publishers
        .iter()
        .filter(|p| !remove_pubs.contains(p))
        .count();
    let remaining_subs = user
        .subscribers
        .iter()
        .filter(|s| !remove_subs.contains(s))
        .count();
    remaining_pubs == 0 && remaining_subs == 0
}

fn warn_idle_tunnel() -> &'static str {
    "⚠️  This leaves your multicast user with no publisher or subscriber roles. \
     The tunnel will remain provisioned but idle — run 'doublezero disconnect' \
     if you want to fully tear it down."
}

impl Subscribe {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        _ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        let client_ip = resolve_client_ip(daemon).await?;
        let spinner = init_spinner(2);
        writeln!(out, "⚡  Subscribing (client_ip: {client_ip})...")?;

        let (user_pk, user) = load_multicast_user(ledger, client_ip)?;
        let groups = resolve_groups(ledger, &self.groups)?;
        spinner.inc(1);

        let mut to_apply = Vec::new();
        for (code, group_pk) in groups {
            if user.subscribers.contains(&group_pk) {
                writeln!(out, "    already subscribed to {code} — skipping")?;
                continue;
            }
            let carry_pub = user.publishers.contains(&group_pk);
            to_apply.push((code, group_pk, (carry_pub, true)));
        }
        let failures = apply_role_update_batches(
            ledger,
            out,
            user_pk,
            client_ip,
            batch_role_updates(to_apply),
            "subscribed to",
            "subscribe to",
        )?;

        finish_update(&spinner, out)?;
        report_failures("subscribe", &failures)
    }
}

impl Unsubscribe {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        _ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        let client_ip = resolve_client_ip(daemon).await?;
        let spinner = init_spinner(2);
        writeln!(out, "⚡  Unsubscribing (client_ip: {client_ip})...")?;

        let (user_pk, user) = load_multicast_user(ledger, client_ip)?;
        let groups = resolve_groups(ledger, &self.groups)?;
        spinner.inc(1);

        let effective_removals: Vec<Pubkey> = groups
            .iter()
            .map(|(_, pk)| *pk)
            .filter(|pk| user.subscribers.contains(pk))
            .collect();

        if would_empty_all_roles(&user, &[], &effective_removals) {
            writeln!(out, "{}", warn_idle_tunnel())?;
        }

        let mut to_apply = Vec::new();
        for (code, group_pk) in groups {
            if !user.subscribers.contains(&group_pk) {
                writeln!(out, "    not subscribed to {code} — skipping")?;
                continue;
            }
            let carry_pub = user.publishers.contains(&group_pk);
            to_apply.push((code, group_pk, (carry_pub, false)));
        }
        let failures = apply_role_update_batches(
            ledger,
            out,
            user_pk,
            client_ip,
            batch_role_updates(to_apply),
            "unsubscribed from",
            "unsubscribe from",
        )?;

        finish_update(&spinner, out)?;
        report_failures("unsubscribe", &failures)
    }
}

impl Publish {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        _ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        let client_ip = resolve_client_ip(daemon).await?;
        let spinner = init_spinner(2);
        writeln!(out, "⚡  Publishing (client_ip: {client_ip})...")?;

        let (user_pk, user) = load_multicast_user(ledger, client_ip)?;
        let groups = resolve_groups(ledger, &self.groups)?;
        spinner.inc(1);

        let mut to_apply = Vec::new();
        for (code, group_pk) in groups {
            if user.publishers.contains(&group_pk) {
                writeln!(out, "    already publishing to {code} — skipping")?;
                continue;
            }
            let carry_sub = user.subscribers.contains(&group_pk);
            to_apply.push((code, group_pk, (true, carry_sub)));
        }
        let failures = apply_role_update_batches(
            ledger,
            out,
            user_pk,
            client_ip,
            batch_role_updates(to_apply),
            "publishing to",
            "publish to",
        )?;

        finish_update(&spinner, out)?;
        report_failures("publish", &failures)
    }
}

impl Unpublish {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        _ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        let client_ip = resolve_client_ip(daemon).await?;
        let spinner = init_spinner(2);
        writeln!(out, "⚡  Unpublishing (client_ip: {client_ip})...")?;

        let (user_pk, user) = load_multicast_user(ledger, client_ip)?;
        let groups = resolve_groups(ledger, &self.groups)?;
        spinner.inc(1);

        // Figure out which of the requested groups the user is actually publishing to.
        let effective_removals: Vec<Pubkey> = groups
            .iter()
            .map(|(_, pk)| *pk)
            .filter(|pk| user.publishers.contains(pk))
            .collect();

        if would_empty_publishers(&user, &effective_removals) {
            writeln!(
                out,
                "⚠️  This removes your last publisher role. In legacy-allocation \
                 environments the service may briefly reprovision while the network \
                 reallocates."
            )?;
        }

        if would_empty_all_roles(&user, &effective_removals, &[]) {
            writeln!(out, "{}", warn_idle_tunnel())?;
        }

        let mut to_apply = Vec::new();
        for (code, group_pk) in groups {
            if !user.publishers.contains(&group_pk) {
                writeln!(out, "    not publishing to {code} — skipping")?;
                continue;
            }
            let carry_sub = user.subscribers.contains(&group_pk);
            to_apply.push((code, group_pk, (false, carry_sub)));
        }
        let failures = apply_role_update_batches(
            ledger,
            out,
            user_pk,
            client_ip,
            batch_role_updates(to_apply),
            "unpublished from",
            "unpublish from",
        )?;

        finish_update(&spinner, out)?;
        report_failures("unpublish", &failures)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        client::{MockDaemonClient, V2StatusResponse},
        ledger::MockLedgerClient,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        AccountType, MulticastGroup, MulticastGroupStatus, User, UserCYOA, UserStatus,
    };
    use std::collections::HashMap;

    fn daemon_with_client_ip(client_ip: &str) -> MockDaemonClient {
        let client_ip = client_ip.to_string();
        let mut daemon = MockDaemonClient::new();
        daemon.expect_v2_status().returning(move || {
            Ok(V2StatusResponse {
                reconciler_enabled: true,
                client_ip: client_ip.clone(),
                network: String::new(),
                services: vec![],
            })
        });
        daemon
    }

    fn make_user(client_ip: Ipv4Addr, user_type: UserType) -> User {
        User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            user_type,
            tenant_pk: Pubkey::default(),
            device_pk: Pubkey::default(),
            cyoa_type: UserCYOA::None,
            client_ip,
            dz_ip: Ipv4Addr::UNSPECIFIED,
            tunnel_id: 0,
            tunnel_net: Default::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            ..Default::default()
        }
    }

    fn user_with_roles(ip: Ipv4Addr, publishers: Vec<Pubkey>, subscribers: Vec<Pubkey>) -> User {
        let mut u = make_user(ip, UserType::Multicast);
        u.publishers = publishers;
        u.subscribers = subscribers;
        u
    }

    fn make_group(code: &str) -> MulticastGroup {
        MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: Pubkey::default(),
            index: 0,
            bump_seed: 0,
            tenant_pk: Pubkey::default(),
            multicast_ip: Ipv4Addr::UNSPECIFIED,
            max_bandwidth: 0,
            status: MulticastGroupStatus::Activated,
            code: code.to_string(),
            publisher_count: 0,
            subscriber_count: 0,
        }
    }

    fn ledger_with_users_and_groups(
        users: HashMap<Pubkey, User>,
        groups: HashMap<Pubkey, MulticastGroup>,
    ) -> MockLedgerClient {
        let mut ledger = MockLedgerClient::new();
        ledger
            .expect_list_user()
            .returning(move || Ok(users.clone()));
        ledger
            .expect_list_multicastgroup()
            .returning(move || Ok(groups.clone()));
        ledger
    }

    #[test]
    fn resolve_groups_returns_pubkeys_in_order() {
        let g1_pk = Pubkey::new_unique();
        let g2_pk = Pubkey::new_unique();
        let mut groups = HashMap::new();
        groups.insert(g1_pk, make_group("g1"));
        groups.insert(g2_pk, make_group("g2"));
        let ledger = ledger_with_users_and_groups(HashMap::new(), groups);

        let out = resolve_groups(&ledger, &["g2".into(), "g1".into()]).unwrap();
        assert_eq!(out, vec![("g2".into(), g2_pk), ("g1".into(), g1_pk)]);
    }

    #[test]
    fn resolve_groups_errors_on_unknown_code() {
        let g1_pk = Pubkey::new_unique();
        let mut groups = HashMap::new();
        groups.insert(g1_pk, make_group("g1"));
        let ledger = ledger_with_users_and_groups(HashMap::new(), groups);

        let err = resolve_groups(&ledger, &["nope".into()]).unwrap_err();
        assert!(
            err.to_string().contains("Multicast group not found: nope"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn load_multicast_user_finds_user_for_client_ip() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let user_pk = Pubkey::new_unique();
        let user = make_user(ip, UserType::Multicast);
        let mut users = HashMap::new();
        users.insert(user_pk, user.clone());
        let ledger = ledger_with_users_and_groups(users, HashMap::new());

        let (pk, loaded) = load_multicast_user(&ledger, ip).unwrap();
        assert_eq!(pk, user_pk);
        assert_eq!(loaded.client_ip, ip);
        assert_eq!(loaded.user_type, UserType::Multicast);
    }

    #[test]
    fn load_multicast_user_errors_when_only_ibrl_user_exists() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let mut users = HashMap::new();
        users.insert(Pubkey::new_unique(), make_user(ip, UserType::IBRL));
        let ledger = ledger_with_users_and_groups(users, HashMap::new());

        let err = load_multicast_user(&ledger, ip).unwrap_err();
        assert!(
            err.to_string().contains("No active multicast user"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn load_multicast_user_errors_when_no_user_for_this_ip() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let other_ip = Ipv4Addr::new(10, 0, 0, 2);
        let mut users = HashMap::new();
        users.insert(
            Pubkey::new_unique(),
            make_user(other_ip, UserType::Multicast),
        );
        let ledger = ledger_with_users_and_groups(users, HashMap::new());

        let err = load_multicast_user(&ledger, ip).unwrap_err();
        assert!(err.to_string().contains("No active multicast user"));
    }

    // --- Unsubscribe tests ---

    #[test]
    fn unsubscribe_removes_subscriber_role_and_preserves_publisher_role() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        // User is BOTH publisher and subscriber of g — unsubscribe must keep publisher=true.
        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![g_pk], vec![g_pk]));
        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        let mut ledger = ledger_with_users_and_groups(users, groups);

        ledger
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.user_pk == user_pk
                    && cmd.group_pks == vec![g_pk]
                    && cmd.client_ip == ip
                    && cmd.publisher
                    && !cmd.subscriber
            })
            .once()
            .returning(|_| Ok(()));

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Unsubscribe {
            groups: vec!["g".into()],
        };
        block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap();

        let rendered = String::from_utf8(out).unwrap();
        assert!(rendered.contains("unsubscribed from g"), "got: {rendered}");
    }

    #[test]
    fn unsubscribe_skips_group_user_is_not_subscribed_to() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![], vec![]));
        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        let mut ledger = ledger_with_users_and_groups(users, groups);
        ledger.expect_update_multicastgroup_roles().never();

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Unsubscribe {
            groups: vec!["g".into()],
        };
        block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap();

        let rendered = String::from_utf8(out).unwrap();
        assert!(
            rendered.contains("not subscribed to g — skipping"),
            "got: {rendered}"
        );
    }

    #[test]
    fn unsubscribe_errors_when_user_missing() {
        let ledger = ledger_with_users_and_groups(HashMap::new(), HashMap::new());
        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Unsubscribe {
            groups: vec!["g".into()],
        };
        let err = block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap_err();
        assert!(err.to_string().contains("No active multicast user"));
    }

    #[test]
    fn unsubscribe_errors_when_daemon_has_no_client_ip() {
        let mut ledger = MockLedgerClient::new();
        ledger.expect_update_multicastgroup_roles().never();
        let daemon = daemon_with_client_ip("");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Unsubscribe {
            groups: vec!["g".into()],
        };
        let err = block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap_err();
        assert!(err.to_string().contains("has not discovered its client IP"));
    }

    #[test]
    fn unsubscribe_errors_on_unknown_group_before_any_onchain_call() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let user_pk = Pubkey::new_unique();

        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![], vec![]));
        let mut ledger = ledger_with_users_and_groups(users, HashMap::new());
        ledger.expect_update_multicastgroup_roles().never();

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Unsubscribe {
            groups: vec!["unknown".into()],
        };
        let err = block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap_err();
        assert!(err
            .to_string()
            .contains("Multicast group not found: unknown"));
    }

    #[test]
    fn unsubscribe_continues_after_per_group_failure_and_aggregates_error() {
        // g1 and g2 carry different publisher flags, so they land in separate
        // batches: g1's batch fails, g2's must still be attempted, and the
        // command must return an aggregated error naming g1.
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g1 = Pubkey::new_unique();
        let g2 = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        // Subscriber of both, publisher of g1 only — unsubscribing g1 carries
        // publisher=true and g2 carries publisher=false.
        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![g1], vec![g1, g2]));
        let mut groups = HashMap::new();
        groups.insert(g1, make_group("g1"));
        groups.insert(g2, make_group("g2"));
        let mut ledger = ledger_with_users_and_groups(users, groups);

        ledger
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.group_pks == vec![g1] && cmd.publisher
            })
            .once()
            .returning(|_| Err(eyre::eyre!("simulated chain failure")));
        ledger
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.group_pks == vec![g2] && !cmd.publisher
            })
            .once()
            .returning(|_| Ok(()));

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Unsubscribe {
            groups: vec!["g1".into(), "g2".into()],
        };
        let err = block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap_err();
        let msg = err.to_string();
        assert!(msg.contains("unsubscribe failed"), "got: {msg}");
        assert!(msg.contains("g1"), "got: {msg}");
        assert!(!msg.contains("g2"), "g2 should have succeeded; got: {msg}");
    }

    // --- Unpublish tests ---

    #[test]
    fn unpublish_removes_publisher_role_and_preserves_subscriber_role() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g1 = Pubkey::new_unique();
        let g2 = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        // Publisher of g1 & g2, subscriber of g1. Unpublish g1 must keep subscriber=true.
        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![g1, g2], vec![g1]));
        let mut groups = HashMap::new();
        groups.insert(g1, make_group("g1"));
        groups.insert(g2, make_group("g2"));
        let mut ledger = ledger_with_users_and_groups(users, groups);

        ledger
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.user_pk == user_pk
                    && cmd.group_pks == vec![g1]
                    && !cmd.publisher
                    && cmd.subscriber
            })
            .once()
            .returning(|_| Ok(()));

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Unpublish {
            groups: vec!["g1".into()],
        };
        block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap();
    }

    #[test]
    fn unpublish_skips_group_user_is_not_publishing_to() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![], vec![]));
        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        let mut ledger = ledger_with_users_and_groups(users, groups);
        ledger.expect_update_multicastgroup_roles().never();

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Unpublish {
            groups: vec!["g".into()],
        };
        block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap();
    }

    #[test]
    fn unpublish_last_publisher_still_issues_onchain_call() {
        // The CLI prints a warning but does not block.
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![g_pk], vec![]));
        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        let mut ledger = ledger_with_users_and_groups(users, groups);

        ledger
            .expect_update_multicastgroup_roles()
            .once()
            .returning(|_| Ok(()));

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Unpublish {
            groups: vec!["g".into()],
        };
        block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap();

        let rendered = String::from_utf8(out).unwrap();
        assert!(
            rendered.contains("removes your last publisher role"),
            "got: {rendered}"
        );
        assert!(
            rendered.contains("no publisher or subscriber roles"),
            "got: {rendered}"
        );
    }

    #[test]
    fn unpublish_of_nonlast_publisher_does_not_claim_last() {
        // would_empty_publishers logic: user has two, remove one — NOT last.
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g1 = Pubkey::new_unique();
        let g2 = Pubkey::new_unique();

        let user = user_with_roles(ip, vec![g1, g2], vec![]);
        assert!(!would_empty_publishers(&user, &[g1]));
        assert!(would_empty_publishers(&user, &[g1, g2]));
    }

    #[test]
    fn would_empty_all_roles_detects_idle_tunnel_cases() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g = Pubkey::new_unique();

        // Only subscriber role; removing it empties everything.
        let sub_only = user_with_roles(ip, vec![], vec![g]);
        assert!(would_empty_all_roles(&sub_only, &[], &[g]));

        // Only publisher role; removing it empties everything.
        let pub_only = user_with_roles(ip, vec![g], vec![]);
        assert!(would_empty_all_roles(&pub_only, &[g], &[]));
    }

    #[test]
    fn would_empty_all_roles_false_when_any_role_remains() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g1 = Pubkey::new_unique();
        let g2 = Pubkey::new_unique();

        // Two subs; removing one leaves the other.
        let two_subs = user_with_roles(ip, vec![], vec![g1, g2]);
        assert!(!would_empty_all_roles(&two_subs, &[], &[g1]));

        // Pub + sub; removing only the sub leaves the pub.
        let both = user_with_roles(ip, vec![g1], vec![g2]);
        assert!(!would_empty_all_roles(&both, &[], &[g2]));
    }

    #[test]
    fn would_empty_all_roles_false_on_no_op() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g = Pubkey::new_unique();

        // Nothing to remove on either side → no-op, don't warn.
        let user = user_with_roles(ip, vec![g], vec![]);
        assert!(!would_empty_all_roles(&user, &[], &[]));

        // Already-empty user + no removals → don't warn.
        let empty = user_with_roles(ip, vec![], vec![]);
        assert!(!would_empty_all_roles(&empty, &[], &[]));
    }

    // --- Subscribe tests ---

    #[test]
    fn subscribe_adds_subscriber_role_and_preserves_publisher_role() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        // User is already a publisher of g — subscribing must keep publisher=true.
        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![g_pk], vec![]));
        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        let mut ledger = ledger_with_users_and_groups(users, groups);

        ledger
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.user_pk == user_pk
                    && cmd.group_pks == vec![g_pk]
                    && cmd.publisher
                    && cmd.subscriber
            })
            .once()
            .returning(|_| Ok(()));

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Subscribe {
            groups: vec!["g".into()],
        };
        block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap();

        let rendered = String::from_utf8(out).unwrap();
        assert!(rendered.contains("subscribed to g"), "got: {rendered}");
        assert!(
            rendered.contains("✅  Updated. Routes will adjust shortly."),
            "got: {rendered}"
        );
    }

    #[test]
    fn subscribe_skips_already_subscribed_group() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![], vec![g_pk]));
        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        let mut ledger = ledger_with_users_and_groups(users, groups);
        ledger.expect_update_multicastgroup_roles().never();

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Subscribe {
            groups: vec!["g".into()],
        };
        block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap();

        let rendered = String::from_utf8(out).unwrap();
        assert!(
            rendered.contains("already subscribed to g — skipping"),
            "got: {rendered}"
        );
    }

    #[test]
    fn subscribe_batches_same_flag_groups_into_one_call() {
        // Neither group carries a publisher role, so both share the
        // (publisher=false, subscriber=true) pair and ride in ONE transaction,
        // in the order the codes were passed.
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g1_pk = Pubkey::new_unique();
        let g2_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![], vec![]));
        let mut groups = HashMap::new();
        groups.insert(g1_pk, make_group("g1"));
        groups.insert(g2_pk, make_group("g2"));
        let mut ledger = ledger_with_users_and_groups(users, groups);

        ledger
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.user_pk == user_pk
                    && cmd.group_pks == vec![g1_pk, g2_pk]
                    && !cmd.publisher
                    && cmd.subscriber
            })
            .once()
            .returning(|_| Ok(()));

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Subscribe {
            groups: vec!["g1".into(), "g2".into()],
        };
        block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap();

        // One transaction, but still one result line per group.
        let rendered = String::from_utf8(out).unwrap();
        assert!(rendered.contains("subscribed to g1"), "got: {rendered}");
        assert!(rendered.contains("subscribed to g2"), "got: {rendered}");
    }

    #[test]
    fn subscribe_failed_batch_reports_every_code_it_carried() {
        // A batch is atomic: when its transaction fails nothing was applied, so
        // every code it carried counts as a failure.
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g1_pk = Pubkey::new_unique();
        let g2_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![], vec![]));
        let mut groups = HashMap::new();
        groups.insert(g1_pk, make_group("g1"));
        groups.insert(g2_pk, make_group("g2"));
        let mut ledger = ledger_with_users_and_groups(users, groups);

        ledger
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.group_pks == vec![g1_pk, g2_pk]
            })
            .once()
            .returning(|_| Err(eyre::eyre!("simulated chain failure")));

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Subscribe {
            groups: vec!["g1".into(), "g2".into()],
        };
        let err = block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap_err();

        let rendered = String::from_utf8(out).unwrap();
        assert!(
            rendered.contains("❌ failed to subscribe to g1, g2"),
            "got: {rendered}"
        );
        let msg = err.to_string();
        assert!(msg.contains("subscribe failed for 2 group(s)"), "got: {msg}");
        assert!(msg.contains("g1") && msg.contains("g2"), "got: {msg}");
    }

    #[test]
    fn unsubscribe_batches_same_flag_groups_into_one_call() {
        // Subscriber-only of both groups, so both carry
        // (publisher=false, subscriber=false) and ride in ONE transaction.
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g1_pk = Pubkey::new_unique();
        let g2_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![], vec![g1_pk, g2_pk]));
        let mut groups = HashMap::new();
        groups.insert(g1_pk, make_group("g1"));
        groups.insert(g2_pk, make_group("g2"));
        let mut ledger = ledger_with_users_and_groups(users, groups);

        ledger
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.user_pk == user_pk
                    && cmd.group_pks == vec![g1_pk, g2_pk]
                    && !cmd.publisher
                    && !cmd.subscriber
            })
            .once()
            .returning(|_| Ok(()));

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Unsubscribe {
            groups: vec!["g1".into(), "g2".into()],
        };
        block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap();

        let rendered = String::from_utf8(out).unwrap();
        assert!(rendered.contains("unsubscribed from g1"), "got: {rendered}");
        assert!(rendered.contains("unsubscribed from g2"), "got: {rendered}");
    }

    // --- Publish tests ---

    #[test]
    fn publish_adds_publisher_role_and_preserves_subscriber_role() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        // User is already a subscriber of g — publishing must keep subscriber=true.
        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![], vec![g_pk]));
        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        let mut ledger = ledger_with_users_and_groups(users, groups);

        ledger
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.user_pk == user_pk
                    && cmd.group_pks == vec![g_pk]
                    && cmd.publisher
                    && cmd.subscriber
            })
            .once()
            .returning(|_| Ok(()));

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Publish {
            groups: vec!["g".into()],
        };
        block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap();

        let rendered = String::from_utf8(out).unwrap();
        assert!(rendered.contains("publishing to g"), "got: {rendered}");
    }

    #[test]
    fn publish_continues_after_per_group_failure_and_aggregates_error() {
        // g1 and g2 carry different subscriber flags, so they land in separate
        // batches: g1's batch fails, g2's must still be attempted, and the
        // command must return an aggregated error naming g1.
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g1 = Pubkey::new_unique();
        let g2 = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        // Subscriber of g1 only — publishing g1 carries subscriber=true and g2
        // carries subscriber=false.
        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![], vec![g1]));
        let mut groups = HashMap::new();
        groups.insert(g1, make_group("g1"));
        groups.insert(g2, make_group("g2"));
        let mut ledger = ledger_with_users_and_groups(users, groups);

        ledger
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.group_pks == vec![g1] && cmd.subscriber
            })
            .once()
            .returning(|_| Err(eyre::eyre!("simulated chain failure")));
        ledger
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.group_pks == vec![g2] && !cmd.subscriber
            })
            .once()
            .returning(|_| Ok(()));

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Publish {
            groups: vec!["g1".into(), "g2".into()],
        };
        let err = block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap_err();
        let msg = err.to_string();
        assert!(msg.contains("publish failed"), "got: {msg}");
        assert!(msg.contains("g1"), "got: {msg}");
        assert!(!msg.contains("g2"), "g2 should have succeeded; got: {msg}");
    }

    #[test]
    fn publish_skips_already_published_group() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut users = HashMap::new();
        users.insert(user_pk, user_with_roles(ip, vec![g_pk], vec![]));
        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        let mut ledger = ledger_with_users_and_groups(users, groups);
        ledger.expect_update_multicastgroup_roles().never();

        let daemon = daemon_with_client_ip("10.0.0.1");
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let cmd = Publish {
            groups: vec!["g".into()],
        };
        block_on(cmd.execute(&ctx, &daemon, &ledger, &mut out)).unwrap();

        let rendered = String::from_utf8(out).unwrap();
        assert!(
            rendered.contains("already publishing to g — skipping"),
            "got: {rendered}"
        );
    }
}
