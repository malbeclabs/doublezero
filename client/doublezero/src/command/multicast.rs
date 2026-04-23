use std::net::Ipv4Addr;

use doublezero_cli::{doublezerocommand::CliCommand, helpers::init_command};
use doublezero_sdk::{
    commands::{
        multicastgroup::{
            list::ListMulticastGroupCommand, subscribe::UpdateMulticastGroupRolesCommand,
        },
        user::list::ListUserCommand,
    },
    User, UserType,
};
use indicatif::ProgressBar;
use solana_sdk::pubkey::Pubkey;

use crate::{
    cli::multicast::{
        MulticastPublishCliCommand, MulticastSubscribeCliCommand, MulticastUnpublishCliCommand,
        MulticastUnsubscribeCliCommand,
    },
    servicecontroller::ServiceControllerImpl,
};

/// Resolve a list of multicast group codes to their on-chain pubkeys.
/// Errors on any unknown code, with no onchain writes.
pub(super) fn resolve_groups(
    client: &dyn CliCommand,
    codes: &[String],
) -> eyre::Result<Vec<(String, Pubkey)>> {
    let mcast_groups = client.list_multicastgroup(ListMulticastGroupCommand)?;
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
pub(super) fn load_multicast_user(
    client: &dyn CliCommand,
    client_ip: Ipv4Addr,
) -> eyre::Result<(Pubkey, User)> {
    let users = client.list_user(ListUserCommand)?;
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

impl MulticastUnsubscribeCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        let client_ip = crate::command::helpers::resolve_client_ip(&controller).await?;
        self.execute_inner(client, client_ip).await
    }

    /// Testable core: takes an already-resolved client_ip.
    async fn execute_inner(self, client: &dyn CliCommand, client_ip: Ipv4Addr) -> eyre::Result<()> {
        let spinner = init_command(2);
        spinner.println(format!("⚡  Unsubscribing (client_ip: {client_ip})..."));

        let (user_pk, user) = load_multicast_user(client, client_ip)?;
        let groups = resolve_groups(client, &self.groups)?;
        spinner.inc(1);

        let effective_removals: Vec<Pubkey> = groups
            .iter()
            .map(|(_, pk)| *pk)
            .filter(|pk| user.subscribers.contains(pk))
            .collect();

        if would_empty_all_roles(&user, &[], &effective_removals) {
            spinner.println(warn_idle_tunnel());
        }

        for (code, group_pk) in groups {
            if !user.subscribers.contains(&group_pk) {
                spinner.println(format!("    not subscribed to {code} — skipping"));
                continue;
            }
            let carry_pub = user.publishers.contains(&group_pk);
            client.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                user_pk,
                group_pk,
                client_ip,
                publisher: carry_pub,
                subscriber: false,
            })?;
            spinner.println(format!("    unsubscribed from {code}"));
        }

        finish_update(&spinner);
        Ok(())
    }
}

fn finish_update(spinner: &ProgressBar) {
    spinner.println("✅  Updated. Routes will adjust shortly.");
    spinner.finish_and_clear();
}

/// Returns true when removing `to_remove` publisher roles from `user` would leave
/// `user.publishers` empty (and the user currently has at least one publisher role).
pub(super) fn would_empty_publishers(user: &User, to_remove: &[Pubkey]) -> bool {
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
pub(super) fn would_empty_all_roles(
    user: &User,
    remove_pubs: &[Pubkey],
    remove_subs: &[Pubkey],
) -> bool {
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

impl MulticastUnpublishCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        let client_ip = crate::command::helpers::resolve_client_ip(&controller).await?;
        self.execute_inner(client, client_ip).await
    }

    async fn execute_inner(self, client: &dyn CliCommand, client_ip: Ipv4Addr) -> eyre::Result<()> {
        let spinner = init_command(2);
        spinner.println(format!("⚡  Unpublishing (client_ip: {client_ip})..."));

        let (user_pk, user) = load_multicast_user(client, client_ip)?;
        let groups = resolve_groups(client, &self.groups)?;
        spinner.inc(1);

        // Figure out which of the requested groups the user is actually publishing to.
        let effective_removals: Vec<Pubkey> = groups
            .iter()
            .map(|(_, pk)| *pk)
            .filter(|pk| user.publishers.contains(pk))
            .collect();

        if would_empty_publishers(&user, &effective_removals) {
            spinner.println(
                "⚠️  This removes your last publisher role. In legacy-allocation \
                 environments the service may briefly reprovision while the network \
                 reallocates.",
            );
        }

        if would_empty_all_roles(&user, &effective_removals, &[]) {
            spinner.println(warn_idle_tunnel());
        }

        for (code, group_pk) in groups {
            if !user.publishers.contains(&group_pk) {
                spinner.println(format!("    not publishing to {code} — skipping"));
                continue;
            }
            let carry_sub = user.subscribers.contains(&group_pk);
            client.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                user_pk,
                group_pk,
                client_ip,
                publisher: false,
                subscriber: carry_sub,
            })?;
            spinner.println(format!("    unpublished from {code}"));
        }

        finish_update(&spinner);
        Ok(())
    }
}

impl MulticastSubscribeCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        let client_ip = crate::command::helpers::resolve_client_ip(&controller).await?;
        self.execute_inner(client, client_ip).await
    }

    async fn execute_inner(self, client: &dyn CliCommand, client_ip: Ipv4Addr) -> eyre::Result<()> {
        let spinner = init_command(2);
        spinner.println(format!("⚡  Subscribing (client_ip: {client_ip})..."));

        let (user_pk, user) = load_multicast_user(client, client_ip)?;
        let groups = resolve_groups(client, &self.groups)?;
        spinner.inc(1);

        for (code, group_pk) in groups {
            if user.subscribers.contains(&group_pk) {
                spinner.println(format!("    already subscribed to {code} — skipping"));
                continue;
            }
            let carry_pub = user.publishers.contains(&group_pk);
            client.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                user_pk,
                group_pk,
                client_ip,
                publisher: carry_pub,
                subscriber: true,
            })?;
            spinner.println(format!("    subscribed to {code}"));
        }

        finish_update(&spinner);
        Ok(())
    }
}

impl MulticastPublishCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        let client_ip = crate::command::helpers::resolve_client_ip(&controller).await?;
        self.execute_inner(client, client_ip).await
    }

    async fn execute_inner(self, client: &dyn CliCommand, client_ip: Ipv4Addr) -> eyre::Result<()> {
        let spinner = init_command(2);
        spinner.println(format!("⚡  Publishing (client_ip: {client_ip})..."));

        let (user_pk, user) = load_multicast_user(client, client_ip)?;
        let groups = resolve_groups(client, &self.groups)?;
        spinner.inc(1);

        for (code, group_pk) in groups {
            if user.publishers.contains(&group_pk) {
                spinner.println(format!("    already publishing to {code} — skipping"));
                continue;
            }
            let carry_sub = user.subscribers.contains(&group_pk);
            client.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                user_pk,
                group_pk,
                client_ip,
                publisher: true,
                subscriber: carry_sub,
            })?;
            spinner.println(format!("    publishing to {code}"));
        }

        finish_update(&spinner);
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cli::multicast::{
        MulticastPublishCliCommand, MulticastSubscribeCliCommand, MulticastUnpublishCliCommand,
        MulticastUnsubscribeCliCommand,
    };
    use doublezero_cli::tests::utils::create_test_client;
    use doublezero_sdk::{
        commands::multicastgroup::subscribe::UpdateMulticastGroupRolesCommand, AccountType,
        MulticastGroup, MulticastGroupStatus, User, UserCYOA, UserStatus,
    };
    use std::collections::HashMap;

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
        }
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

    #[test]
    fn resolve_groups_returns_pubkeys_in_order() {
        let mut client = create_test_client();
        let g1_pk = Pubkey::new_unique();
        let g2_pk = Pubkey::new_unique();
        let mut groups = HashMap::new();
        groups.insert(g1_pk, make_group("g1"));
        groups.insert(g2_pk, make_group("g2"));
        client
            .expect_list_multicastgroup()
            .returning(move |_| Ok(groups.clone()));

        let out = resolve_groups(&client, &["g2".into(), "g1".into()]).unwrap();
        assert_eq!(out, vec![("g2".into(), g2_pk), ("g1".into(), g1_pk)]);
    }

    #[test]
    fn resolve_groups_errors_on_unknown_code() {
        let mut client = create_test_client();
        let g1_pk = Pubkey::new_unique();
        let mut groups = HashMap::new();
        groups.insert(g1_pk, make_group("g1"));
        client
            .expect_list_multicastgroup()
            .returning(move |_| Ok(groups.clone()));

        let err = resolve_groups(&client, &["nope".into()]).unwrap_err();
        assert!(
            err.to_string().contains("Multicast group not found: nope"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn load_multicast_user_finds_user_for_client_ip() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let mut client = create_test_client();
        let user_pk = Pubkey::new_unique();
        let user = make_user(ip, UserType::Multicast);
        let mut users = HashMap::new();
        users.insert(user_pk, user.clone());
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let (pk, loaded) = load_multicast_user(&client, ip).unwrap();
        assert_eq!(pk, user_pk);
        assert_eq!(loaded.client_ip, ip);
        assert_eq!(loaded.user_type, UserType::Multicast);
    }

    #[test]
    fn load_multicast_user_errors_when_only_ibrl_user_exists() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let mut client = create_test_client();
        let mut users = HashMap::new();
        users.insert(Pubkey::new_unique(), make_user(ip, UserType::IBRL));
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let err = load_multicast_user(&client, ip).unwrap_err();
        assert!(
            err.to_string().contains("No active multicast user"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn load_multicast_user_errors_when_no_user_for_this_ip() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let other_ip = Ipv4Addr::new(10, 0, 0, 2);
        let mut client = create_test_client();
        let mut users = HashMap::new();
        users.insert(
            Pubkey::new_unique(),
            make_user(other_ip, UserType::Multicast),
        );
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let err = load_multicast_user(&client, ip).unwrap_err();
        assert!(err.to_string().contains("No active multicast user"));
    }

    // --- MulticastUnsubscribeCliCommand tests ---

    fn user_with_roles(ip: Ipv4Addr, publishers: Vec<Pubkey>, subscribers: Vec<Pubkey>) -> User {
        let mut u = make_user(ip, UserType::Multicast);
        u.publishers = publishers;
        u.subscribers = subscribers;
        u
    }

    #[tokio::test]
    async fn unsubscribe_removes_subscriber_role_and_preserves_publisher_role() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut client = create_test_client();
        // User is BOTH publisher and subscriber of g — unsubscribe must keep publisher=true.
        let user = user_with_roles(ip, vec![g_pk], vec![g_pk]);
        let mut users = HashMap::new();
        users.insert(user_pk, user);
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        client
            .expect_list_multicastgroup()
            .returning(move |_| Ok(groups.clone()));

        client
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.user_pk == user_pk
                    && cmd.group_pk == g_pk
                    && cmd.client_ip == ip
                    && cmd.publisher
                    && !cmd.subscriber
            })
            .once()
            .returning(|_| Ok(solana_sdk::signature::Signature::default()));

        let cmd = MulticastUnsubscribeCliCommand {
            groups: vec!["g".into()],
        };
        cmd.execute_inner(&client, ip).await.unwrap();
    }

    #[tokio::test]
    async fn unsubscribe_skips_group_user_is_not_subscribed_to() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut client = create_test_client();
        let user = user_with_roles(ip, vec![], vec![]);
        let mut users = HashMap::new();
        users.insert(user_pk, user);
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        client
            .expect_list_multicastgroup()
            .returning(move |_| Ok(groups.clone()));

        client.expect_update_multicastgroup_roles().never();

        let cmd = MulticastUnsubscribeCliCommand {
            groups: vec!["g".into()],
        };
        cmd.execute_inner(&client, ip).await.unwrap();
    }

    #[tokio::test]
    async fn unsubscribe_errors_when_user_missing() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let mut client = create_test_client();
        client.expect_list_user().returning(|_| Ok(HashMap::new()));

        let cmd = MulticastUnsubscribeCliCommand {
            groups: vec!["g".into()],
        };
        let err = cmd.execute_inner(&client, ip).await.unwrap_err();
        assert!(err.to_string().contains("No active multicast user"));
    }

    #[tokio::test]
    async fn unsubscribe_errors_on_unknown_group_before_any_onchain_call() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let user_pk = Pubkey::new_unique();
        let mut client = create_test_client();

        let user = user_with_roles(ip, vec![], vec![]);
        let mut users = HashMap::new();
        users.insert(user_pk, user);
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        client
            .expect_list_multicastgroup()
            .returning(|_| Ok(HashMap::new()));
        client.expect_update_multicastgroup_roles().never();

        let cmd = MulticastUnsubscribeCliCommand {
            groups: vec!["unknown".into()],
        };
        let err = cmd.execute_inner(&client, ip).await.unwrap_err();
        assert!(err
            .to_string()
            .contains("Multicast group not found: unknown"));
    }

    // --- MulticastUnpublishCliCommand tests ---

    #[tokio::test]
    async fn unpublish_removes_publisher_role_and_preserves_subscriber_role() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g1 = Pubkey::new_unique();
        let g2 = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut client = create_test_client();
        // Publisher of g1 & g2, subscriber of g1. Unpublish g1 must keep subscriber=true.
        let user = user_with_roles(ip, vec![g1, g2], vec![g1]);
        let mut users = HashMap::new();
        users.insert(user_pk, user);
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let mut groups = HashMap::new();
        groups.insert(g1, make_group("g1"));
        groups.insert(g2, make_group("g2"));
        client
            .expect_list_multicastgroup()
            .returning(move |_| Ok(groups.clone()));

        client
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.user_pk == user_pk && cmd.group_pk == g1 && !cmd.publisher && cmd.subscriber
            })
            .once()
            .returning(|_| Ok(solana_sdk::signature::Signature::default()));

        let cmd = MulticastUnpublishCliCommand {
            groups: vec!["g1".into()],
        };
        cmd.execute_inner(&client, ip).await.unwrap();
    }

    #[tokio::test]
    async fn unpublish_skips_group_user_is_not_publishing_to() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut client = create_test_client();
        let user = user_with_roles(ip, vec![], vec![]);
        let mut users = HashMap::new();
        users.insert(user_pk, user);
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        client
            .expect_list_multicastgroup()
            .returning(move |_| Ok(groups.clone()));

        client.expect_update_multicastgroup_roles().never();

        let cmd = MulticastUnpublishCliCommand {
            groups: vec!["g".into()],
        };
        cmd.execute_inner(&client, ip).await.unwrap();
    }

    #[tokio::test]
    async fn unpublish_last_publisher_still_issues_onchain_call() {
        // The CLI prints a warning but does not block.
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut client = create_test_client();
        let user = user_with_roles(ip, vec![g_pk], vec![]);
        let mut users = HashMap::new();
        users.insert(user_pk, user);
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        client
            .expect_list_multicastgroup()
            .returning(move |_| Ok(groups.clone()));

        client
            .expect_update_multicastgroup_roles()
            .once()
            .returning(|_| Ok(solana_sdk::signature::Signature::default()));

        let cmd = MulticastUnpublishCliCommand {
            groups: vec!["g".into()],
        };
        cmd.execute_inner(&client, ip).await.unwrap();
    }

    #[tokio::test]
    async fn unpublish_of_nonlast_publisher_does_not_claim_last() {
        // would_empty_publishers logic: user has two, remove one — NOT last.
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g1 = Pubkey::new_unique();
        let g2 = Pubkey::new_unique();

        let user = user_with_roles(ip, vec![g1, g2], vec![]);
        let would_empty = super::would_empty_publishers(&user, &[g1]);
        assert!(!would_empty);

        let would_empty_all = super::would_empty_publishers(&user, &[g1, g2]);
        assert!(would_empty_all);
    }

    #[test]
    fn would_empty_all_roles_detects_idle_tunnel_cases() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g = Pubkey::new_unique();

        // Only subscriber role; removing it empties everything.
        let sub_only = user_with_roles(ip, vec![], vec![g]);
        assert!(super::would_empty_all_roles(&sub_only, &[], &[g]));

        // Only publisher role; removing it empties everything.
        let pub_only = user_with_roles(ip, vec![g], vec![]);
        assert!(super::would_empty_all_roles(&pub_only, &[g], &[]));
    }

    #[test]
    fn would_empty_all_roles_false_when_any_role_remains() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g1 = Pubkey::new_unique();
        let g2 = Pubkey::new_unique();

        // Two subs; removing one leaves the other.
        let two_subs = user_with_roles(ip, vec![], vec![g1, g2]);
        assert!(!super::would_empty_all_roles(&two_subs, &[], &[g1]));

        // Pub + sub; removing only the sub leaves the pub.
        let both = user_with_roles(ip, vec![g1], vec![g2]);
        assert!(!super::would_empty_all_roles(&both, &[], &[g2]));
    }

    #[test]
    fn would_empty_all_roles_false_on_no_op() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g = Pubkey::new_unique();

        // Nothing to remove on either side → no-op, don't warn.
        let user = user_with_roles(ip, vec![g], vec![]);
        assert!(!super::would_empty_all_roles(&user, &[], &[]));

        // Already-empty user + no removals → don't warn.
        let empty = user_with_roles(ip, vec![], vec![]);
        assert!(!super::would_empty_all_roles(&empty, &[], &[]));
    }

    // --- MulticastSubscribeCliCommand tests ---

    #[tokio::test]
    async fn subscribe_adds_subscriber_role_and_preserves_publisher_role() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut client = create_test_client();
        // User is already a publisher of g — subscribing must keep publisher=true.
        let user = user_with_roles(ip, vec![g_pk], vec![]);
        let mut users = HashMap::new();
        users.insert(user_pk, user);
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        client
            .expect_list_multicastgroup()
            .returning(move |_| Ok(groups.clone()));

        client
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.user_pk == user_pk && cmd.group_pk == g_pk && cmd.publisher && cmd.subscriber
            })
            .once()
            .returning(|_| Ok(solana_sdk::signature::Signature::default()));

        let cmd = MulticastSubscribeCliCommand {
            groups: vec!["g".into()],
        };
        cmd.execute_inner(&client, ip).await.unwrap();
    }

    #[tokio::test]
    async fn subscribe_skips_already_subscribed_group() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut client = create_test_client();
        let user = user_with_roles(ip, vec![], vec![g_pk]);
        let mut users = HashMap::new();
        users.insert(user_pk, user);
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        client
            .expect_list_multicastgroup()
            .returning(move |_| Ok(groups.clone()));

        client.expect_update_multicastgroup_roles().never();

        let cmd = MulticastSubscribeCliCommand {
            groups: vec!["g".into()],
        };
        cmd.execute_inner(&client, ip).await.unwrap();
    }

    // --- MulticastPublishCliCommand tests ---

    #[tokio::test]
    async fn publish_adds_publisher_role_and_preserves_subscriber_role() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut client = create_test_client();
        // User is already a subscriber of g — publishing must keep subscriber=true.
        let user = user_with_roles(ip, vec![], vec![g_pk]);
        let mut users = HashMap::new();
        users.insert(user_pk, user);
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        client
            .expect_list_multicastgroup()
            .returning(move |_| Ok(groups.clone()));

        client
            .expect_update_multicastgroup_roles()
            .withf(move |cmd: &UpdateMulticastGroupRolesCommand| {
                cmd.user_pk == user_pk && cmd.group_pk == g_pk && cmd.publisher && cmd.subscriber
            })
            .once()
            .returning(|_| Ok(solana_sdk::signature::Signature::default()));

        let cmd = MulticastPublishCliCommand {
            groups: vec!["g".into()],
        };
        cmd.execute_inner(&client, ip).await.unwrap();
    }

    #[tokio::test]
    async fn publish_skips_already_published_group() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g_pk = Pubkey::new_unique();
        let user_pk = Pubkey::new_unique();

        let mut client = create_test_client();
        let user = user_with_roles(ip, vec![g_pk], vec![]);
        let mut users = HashMap::new();
        users.insert(user_pk, user);
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let mut groups = HashMap::new();
        groups.insert(g_pk, make_group("g"));
        client
            .expect_list_multicastgroup()
            .returning(move |_| Ok(groups.clone()));

        client.expect_update_multicastgroup_roles().never();

        let cmd = MulticastPublishCliCommand {
            groups: vec!["g".into()],
        };
        cmd.execute_inner(&client, ip).await.unwrap();
    }
}
