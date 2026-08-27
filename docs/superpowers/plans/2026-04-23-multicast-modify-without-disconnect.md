# Multicast: modify subscriptions without disconnecting — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add four CLI subcommands under `doublezero multicast` — `subscribe`, `unsubscribe`, `publish`, `unpublish` — so a connected user can modify their multicast role set without running `disconnect`.

**Architecture:** Extend the client CLI only. Each verb resolves group codes to pubkeys, loads the caller's existing Multicast user, and issues one `UpdateMulticastGroupRolesCommand` per group with the correct boolean flags (carrying through the other role's current value). The smartcontract is unchanged; the daemon reconciles multicast routes asynchronously on its next poll.

**Tech Stack:** Rust (clap, eyre, mockall tests), Go (e2e tests via testcontainers + cEOS).

**Spec:** `docs/superpowers/specs/2026-04-23-multicast-modify-without-disconnect-design.md`

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `client/doublezero/src/cli/multicast.rs` | Add four new `MulticastCommands` variants + arg structs |
| Create | `client/doublezero/src/command/multicast.rs` | Handlers for the four verbs + shared helpers |
| Modify | `client/doublezero/src/command/mod.rs` | Register new `multicast` module |
| Modify | `client/doublezero/src/main.rs` | Dispatch new variants under `Command::Multicast` |
| Modify | `e2e/main_test.go` | Add `TestDevnet` helpers for the four new verbs |
| Modify | `e2e/multicast_test.go` | Extend `TestE2E_Multicast` with unsubscribe/unpublish sub-tests |

---

### Task 1: Extend `MulticastCommands` with four new variants

**Files:**
- Modify: `client/doublezero/src/cli/multicast.rs` (entire file is 16 lines)

- [ ] **Step 1: Replace the file with the expanded enum**

Overwrite `client/doublezero/src/cli/multicast.rs` with:

```rust
use clap::{Args, Subcommand};

use super::multicastgroup::MulticastGroupCliCommand;

#[derive(Args, Debug)]
pub struct MulticastCliCommand {
    #[command(subcommand)]
    pub command: MulticastCommands,
}

#[derive(Debug, Subcommand)]
pub enum MulticastCommands {
    /// Manage multicast groups
    #[clap()]
    Group(MulticastGroupCliCommand),
    /// Subscribe to one or more multicast groups (user must already be connected)
    #[clap()]
    Subscribe(MulticastSubscribeCliCommand),
    /// Unsubscribe from one or more multicast groups
    #[clap()]
    Unsubscribe(MulticastUnsubscribeCliCommand),
    /// Publish to one or more multicast groups (user must already be connected)
    #[clap()]
    Publish(MulticastPublishCliCommand),
    /// Stop publishing to one or more multicast groups
    #[clap()]
    Unpublish(MulticastUnpublishCliCommand),
}

#[derive(Args, Debug)]
pub struct MulticastSubscribeCliCommand {
    /// Multicast group code(s) to subscribe to
    #[arg(num_args = 1..)]
    pub groups: Vec<String>,
}

#[derive(Args, Debug)]
pub struct MulticastUnsubscribeCliCommand {
    /// Multicast group code(s) to unsubscribe from
    #[arg(num_args = 1..)]
    pub groups: Vec<String>,
}

#[derive(Args, Debug)]
pub struct MulticastPublishCliCommand {
    /// Multicast group code(s) to publish to
    #[arg(num_args = 1..)]
    pub groups: Vec<String>,
}

#[derive(Args, Debug)]
pub struct MulticastUnpublishCliCommand {
    /// Multicast group code(s) to stop publishing to
    #[arg(num_args = 1..)]
    pub groups: Vec<String>,
}
```

- [ ] **Step 2: Verify clap parsing compiles and `--help` lists the new verbs**

Run:

```bash
cargo check -p doublezero
```

Expected: compiles (may warn about unused `MulticastSubscribeCliCommand` etc. until Task 7 — that's fine).

- [ ] **Step 3: Commit**

```bash
git add client/doublezero/src/cli/multicast.rs
git commit -m "client/doublezero: add multicast subscribe/unsubscribe/publish/unpublish CLI variants"
```

---

### Task 2: Create command module skeleton with shared helpers

**Files:**
- Create: `client/doublezero/src/command/multicast.rs`
- Modify: `client/doublezero/src/command/mod.rs`

- [ ] **Step 1: Add module registration**

Edit `client/doublezero/src/command/mod.rs` to add `pub mod multicast;` alphabetically. The file should then read:

```rust
pub mod connect;
pub mod disable;
pub mod disconnect;
pub mod enable;
pub mod helpers;
pub mod latency;
pub mod multicast;
pub mod routes;
pub mod status;
pub mod util;
```

- [ ] **Step 2: Write the failing tests**

Create `client/doublezero/src/command/multicast.rs`:

```rust
use std::net::Ipv4Addr;

use doublezero_cli::doublezerocommand::CliCommand;
use doublezero_sdk::{
    commands::{
        multicastgroup::list::ListMulticastGroupCommand, user::list::ListUserCommand,
    },
    User, UserType,
};
use solana_sdk::pubkey::Pubkey;

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

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_cli::tests::utils::create_test_client;
    use doublezero_sdk::{AccountType, MulticastGroup, MulticastGroupStatus, User, UserCYOA, UserStatus};
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
            code: code.to_string(),
            max_bandwidth: 0,
            status: MulticastGroupStatus::Activated,
            multicast_ip: Ipv4Addr::UNSPECIFIED,
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
        users.insert(Pubkey::new_unique(), make_user(other_ip, UserType::Multicast));
        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));

        let err = load_multicast_user(&client, ip).unwrap_err();
        assert!(err.to_string().contains("No active multicast user"));
    }
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run:

```bash
cargo test -p doublezero --lib command::multicast
```

Expected: tests in this new module compile and run; they should pass immediately because the helpers are implemented. If any fail, fix before committing.

- [ ] **Step 4: Run lint to confirm clean**

Run:

```bash
make rust-fmt && cargo clippy -p doublezero --all-targets -- -D warnings
```

Expected: no warnings/errors in the new file.

- [ ] **Step 5: Commit**

```bash
git add client/doublezero/src/command/mod.rs client/doublezero/src/command/multicast.rs
git commit -m "client/doublezero: add multicast command module with resolve_groups and load_multicast_user helpers"
```

---

### Task 3: Implement `unsubscribe` handler

**Files:**
- Modify: `client/doublezero/src/command/multicast.rs`

- [ ] **Step 1: Write the failing tests**

Append inside the existing `#[cfg(test)] mod tests { ... }` block in `client/doublezero/src/command/multicast.rs`:

```rust
    // --- MulticastUnsubscribeCliCommand tests ---

    use crate::cli::multicast::MulticastUnsubscribeCliCommand;
    use doublezero_sdk::commands::multicastgroup::subscribe::UpdateMulticastGroupRolesCommand;

    fn user_with_roles(
        ip: Ipv4Addr,
        publishers: Vec<Pubkey>,
        subscribers: Vec<Pubkey>,
    ) -> User {
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
                    && cmd.publisher    // carry-through preserved
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
        // User has no sub roles — should no-op without an onchain call.
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
        assert!(err.to_string().contains("Multicast group not found: unknown"));
    }
```

- [ ] **Step 2: Run tests to verify they fail with "no method named `execute_inner`"**

Run:

```bash
cargo test -p doublezero --lib command::multicast::tests::unsubscribe
```

Expected: compile error — `MulticastUnsubscribeCliCommand::execute_inner` does not exist.

- [ ] **Step 3: Implement the handler**

Append to `client/doublezero/src/command/multicast.rs` (above the `#[cfg(test)]` block):

```rust
use doublezero_sdk::commands::multicastgroup::subscribe::UpdateMulticastGroupRolesCommand;

use crate::{
    cli::multicast::MulticastUnsubscribeCliCommand, servicecontroller::ServiceControllerImpl,
};
use doublezero_cli::helpers::init_command;
use indicatif::ProgressBar;

impl MulticastUnsubscribeCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        let client_ip = crate::command::helpers::resolve_client_ip(&controller).await?;
        self.execute_inner(client, client_ip).await
    }

    /// Testable core: takes an already-resolved client_ip.
    async fn execute_inner(
        self,
        client: &dyn CliCommand,
        client_ip: Ipv4Addr,
    ) -> eyre::Result<()> {
        let spinner = init_command(2);
        spinner.println(format!("⚡  Unsubscribing (client_ip: {client_ip})..."));

        let (user_pk, user) = load_multicast_user(client, client_ip)?;
        let groups = resolve_groups(client, &self.groups)?;
        spinner.inc(1);

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
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
cargo test -p doublezero --lib command::multicast::tests::unsubscribe
```

Expected: all four unsubscribe tests PASS.

- [ ] **Step 5: Lint clean**

Run:

```bash
make rust-fmt && cargo clippy -p doublezero --all-targets -- -D warnings
```

Expected: no warnings.

- [ ] **Step 6: Commit**

```bash
git add client/doublezero/src/command/multicast.rs
git commit -m "client/doublezero: implement multicast unsubscribe command"
```

---

### Task 4: Implement `unpublish` handler with last-publisher warning

**Files:**
- Modify: `client/doublezero/src/command/multicast.rs`

- [ ] **Step 1: Write the failing tests**

Append inside the existing `tests` module:

```rust
    // --- MulticastUnpublishCliCommand tests ---

    use crate::cli::multicast::MulticastUnpublishCliCommand;

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
                cmd.user_pk == user_pk
                    && cmd.group_pk == g1
                    && !cmd.publisher
                    && cmd.subscriber    // carry-through preserved
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
        // Would_empty_publishers logic: user has two, remove one — NOT last.
        // Regression check: the helper should return false.
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let g1 = Pubkey::new_unique();
        let g2 = Pubkey::new_unique();

        let user = user_with_roles(ip, vec![g1, g2], vec![]);
        let would_empty = super::would_empty_publishers(&user, &[g1]);
        assert!(!would_empty);

        let would_empty_all = super::would_empty_publishers(&user, &[g1, g2]);
        assert!(would_empty_all);
    }
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
cargo test -p doublezero --lib command::multicast::tests::unpublish
```

Expected: compile errors — `MulticastUnpublishCliCommand::execute_inner` and `would_empty_publishers` do not exist.

- [ ] **Step 3: Implement the handler and helper**

Append to `client/doublezero/src/command/multicast.rs` (above the `#[cfg(test)]` block):

```rust
use crate::cli::multicast::MulticastUnpublishCliCommand;

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

impl MulticastUnpublishCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        let client_ip = crate::command::helpers::resolve_client_ip(&controller).await?;
        self.execute_inner(client, client_ip).await
    }

    async fn execute_inner(
        self,
        client: &dyn CliCommand,
        client_ip: Ipv4Addr,
    ) -> eyre::Result<()> {
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
cargo test -p doublezero --lib command::multicast::tests::unpublish
```

Expected: all four unpublish tests PASS.

- [ ] **Step 5: Lint clean**

Run:

```bash
make rust-fmt && cargo clippy -p doublezero --all-targets -- -D warnings
```

Expected: no warnings.

- [ ] **Step 6: Commit**

```bash
git add client/doublezero/src/command/multicast.rs
git commit -m "client/doublezero: implement multicast unpublish command with last-publisher warning"
```

---

### Task 5: Implement `subscribe` handler

**Files:**
- Modify: `client/doublezero/src/command/multicast.rs`

- [ ] **Step 1: Write the failing tests**

Append inside the existing `tests` module:

```rust
    // --- MulticastSubscribeCliCommand tests ---

    use crate::cli::multicast::MulticastSubscribeCliCommand;

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
                cmd.user_pk == user_pk
                    && cmd.group_pk == g_pk
                    && cmd.publisher   // carry-through preserved
                    && cmd.subscriber
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
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
cargo test -p doublezero --lib command::multicast::tests::subscribe
```

Expected: compile error — `MulticastSubscribeCliCommand::execute_inner` does not exist.

- [ ] **Step 3: Implement the handler**

Append to `client/doublezero/src/command/multicast.rs` (above the `#[cfg(test)]` block):

```rust
use crate::cli::multicast::MulticastSubscribeCliCommand;

impl MulticastSubscribeCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        let client_ip = crate::command::helpers::resolve_client_ip(&controller).await?;
        self.execute_inner(client, client_ip).await
    }

    async fn execute_inner(
        self,
        client: &dyn CliCommand,
        client_ip: Ipv4Addr,
    ) -> eyre::Result<()> {
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
cargo test -p doublezero --lib command::multicast::tests::subscribe
```

Expected: both subscribe tests PASS.

- [ ] **Step 5: Commit**

```bash
git add client/doublezero/src/command/multicast.rs
git commit -m "client/doublezero: implement multicast subscribe command"
```

---

### Task 6: Implement `publish` handler

**Files:**
- Modify: `client/doublezero/src/command/multicast.rs`

- [ ] **Step 1: Write the failing tests**

Append inside the existing `tests` module:

```rust
    // --- MulticastPublishCliCommand tests ---

    use crate::cli::multicast::MulticastPublishCliCommand;

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
                cmd.user_pk == user_pk
                    && cmd.group_pk == g_pk
                    && cmd.publisher
                    && cmd.subscriber   // carry-through preserved
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
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
cargo test -p doublezero --lib command::multicast::tests::publish
```

Expected: compile error — `MulticastPublishCliCommand::execute_inner` does not exist.

- [ ] **Step 3: Implement the handler**

Append to `client/doublezero/src/command/multicast.rs` (above the `#[cfg(test)]` block):

```rust
use crate::cli::multicast::MulticastPublishCliCommand;

impl MulticastPublishCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        let client_ip = crate::command::helpers::resolve_client_ip(&controller).await?;
        self.execute_inner(client, client_ip).await
    }

    async fn execute_inner(
        self,
        client: &dyn CliCommand,
        client_ip: Ipv4Addr,
    ) -> eyre::Result<()> {
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
cargo test -p doublezero --lib command::multicast
```

Expected: all multicast command tests PASS (subscribe, unsubscribe, publish, unpublish, helpers).

- [ ] **Step 5: Lint clean**

Run:

```bash
make rust-fmt && cargo clippy -p doublezero --all-targets -- -D warnings
```

Expected: no warnings.

- [ ] **Step 6: Commit**

```bash
git add client/doublezero/src/command/multicast.rs
git commit -m "client/doublezero: implement multicast publish command"
```

---

### Task 7: Wire up dispatch in main.rs

**Files:**
- Modify: `client/doublezero/src/main.rs:265-313` (the existing `Command::Multicast` match arm)

- [ ] **Step 1: Extend the match arm**

Find the existing `Command::Multicast(args) => match args.command { ... }` block (around line 265). The existing inner `match` has one arm for `MulticastCommands::Group(...)`. Add four new arms for the new variants *before* the closing `},` of the outer match arm:

Change:

```rust
        Command::Multicast(args) => match args.command {
            cli::multicast::MulticastCommands::Group(args) => match args.command {
                // ... existing group subcommand handling ...
            },
        },
```

To:

```rust
        Command::Multicast(args) => match args.command {
            cli::multicast::MulticastCommands::Group(args) => match args.command {
                // ... existing group subcommand handling unchanged ...
            },
            cli::multicast::MulticastCommands::Subscribe(args) => args.execute(&client).await,
            cli::multicast::MulticastCommands::Unsubscribe(args) => args.execute(&client).await,
            cli::multicast::MulticastCommands::Publish(args) => args.execute(&client).await,
            cli::multicast::MulticastCommands::Unpublish(args) => args.execute(&client).await,
        },
```

- [ ] **Step 2: Build and verify the CLI help text**

Run:

```bash
cargo build -p doublezero && \
  ./target/debug/doublezero multicast --help
```

Expected: the `--help` output lists `group`, `subscribe`, `unsubscribe`, `publish`, `unpublish` subcommands.

- [ ] **Step 3: Run full workspace build and lint**

Run:

```bash
make rust-lint
```

Expected: no errors or warnings.

- [ ] **Step 4: Commit**

```bash
git add client/doublezero/src/main.rs
git commit -m "client/doublezero: wire up multicast subscribe/unsubscribe/publish/unpublish dispatch"
```

---

### Task 8: Add E2E helper methods for the four verbs

**Files:**
- Modify: `e2e/main_test.go` (after the existing `AddMulticastSubscriberGroupSkipAccessPass` helper around line 551-559)

- [ ] **Step 1: Add the four helpers**

Insert after the existing `AddMulticastSubscriberGroupSkipAccessPass` function (around line 559), before `DisconnectMulticastSubscriber`:

```go
// SubscribeMulticastGroup adds subscriber role(s) to an already-connected multicast user.
func (dn *TestDevnet) SubscribeMulticastGroup(t *testing.T, client *devnet.Client, multicastGroupCodes ...string) {
	dn.log.Debug("==> Subscribing to multicast groups", "clientIP", client.CYOANetworkIP, "groups", multicastGroupCodes)

	groupArgs := strings.Join(multicastGroupCodes, " ")
	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero multicast subscribe " + groupArgs})
	require.NoError(t, err, "failed to subscribe to multicast groups")

	dn.log.Debug("--> Subscribed to multicast groups")
}

// UnsubscribeMulticastGroup removes subscriber role(s) from an already-connected multicast user.
func (dn *TestDevnet) UnsubscribeMulticastGroup(t *testing.T, client *devnet.Client, multicastGroupCodes ...string) {
	dn.log.Debug("==> Unsubscribing from multicast groups", "clientIP", client.CYOANetworkIP, "groups", multicastGroupCodes)

	groupArgs := strings.Join(multicastGroupCodes, " ")
	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero multicast unsubscribe " + groupArgs})
	require.NoError(t, err, "failed to unsubscribe from multicast groups")

	dn.log.Debug("--> Unsubscribed from multicast groups")
}

// PublishMulticastGroup adds publisher role(s) to an already-connected multicast user.
func (dn *TestDevnet) PublishMulticastGroup(t *testing.T, client *devnet.Client, multicastGroupCodes ...string) {
	dn.log.Debug("==> Publishing to multicast groups", "clientIP", client.CYOANetworkIP, "groups", multicastGroupCodes)

	groupArgs := strings.Join(multicastGroupCodes, " ")
	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero multicast publish " + groupArgs})
	require.NoError(t, err, "failed to publish to multicast groups")

	dn.log.Debug("--> Published to multicast groups")
}

// UnpublishMulticastGroup removes publisher role(s) from an already-connected multicast user.
func (dn *TestDevnet) UnpublishMulticastGroup(t *testing.T, client *devnet.Client, multicastGroupCodes ...string) {
	dn.log.Debug("==> Unpublishing from multicast groups", "clientIP", client.CYOANetworkIP, "groups", multicastGroupCodes)

	groupArgs := strings.Join(multicastGroupCodes, " ")
	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero multicast unpublish " + groupArgs})
	require.NoError(t, err, "failed to unpublish from multicast groups")

	dn.log.Debug("--> Unpublished from multicast groups")
}
```

- [ ] **Step 2: Verify Go build with e2e tag**

Run:

```bash
go build -tags e2e ./e2e/...
```

Expected: compiles cleanly.

- [ ] **Step 3: Commit**

```bash
git add e2e/main_test.go
git commit -m "e2e: add multicast subscribe/unsubscribe/publish/unpublish test helpers"
```

---

### Task 9: Extend `TestE2E_Multicast` with unsubscribe/unpublish sub-tests

**Files:**
- Modify: `e2e/multicast_test.go` (add a new sub-test block after the existing `connect` block around lines 194-226)

**Context:** The existing `TestE2E_Multicast` connects a publisher to `mg01`, adds `mg02` incrementally, connects a subscriber to `mg01`, adds `mg02` incrementally, and then disconnects both. We'll insert a new sub-test between `connect` and `disconnect` that removes *one of two* roles (so we don't trip the last-publisher `Updating` teardown path) and verifies state.

- [ ] **Step 1: Write the failing sub-test**

In `e2e/multicast_test.go`, locate the `if !t.Run("disconnect", ...)` block (around line 228). Insert a new `modify_roles` sub-test *before* it:

```go
	if !t.Run("modify_roles", func(t *testing.T) {
		// doublezero user list renders multicast memberships in a column named `groups`
		// with each entry prefixed "P:<code>" (publisher) or "S:<code>" (subscriber).
		// See smartcontract/cli/src/user/list.rs::format_multicast_group_names.

		subscriberRow := func() map[string]string {
			out, err := dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "list"})
			if err != nil {
				return nil
			}
			for _, row := range fixtures.ParseCLITable(out) {
				if row["user_type"] == "Multicast" && row["client_ip"] == subscriberClient.CYOANetworkIP {
					return row
				}
			}
			return nil
		}
		publisherRow := func() map[string]string {
			out, err := dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "list"})
			if err != nil {
				return nil
			}
			for _, row := range fixtures.ParseCLITable(out) {
				if row["user_type"] == "Multicast" && row["client_ip"] == publisherClient.CYOANetworkIP {
					return row
				}
			}
			return nil
		}

		// Unsubscribe the subscriber from mg01 (keeps it subscribed to mg02).
		tdn.UnsubscribeMulticastGroup(t, subscriberClient, "mg01")

		require.Eventually(t, func() bool {
			row := subscriberRow()
			if row == nil {
				return false
			}
			groups := row["groups"]
			return !strings.Contains(groups, "S:mg01") && strings.Contains(groups, "S:mg02")
		}, 60*time.Second, 2*time.Second, "subscriber should be unsubscribed from mg01 but still subscribed to mg02")

		// Unpublish the publisher from mg01 (keeps it publishing to mg02).
		tdn.UnpublishMulticastGroup(t, publisherClient, "mg01")

		require.Eventually(t, func() bool {
			row := publisherRow()
			if row == nil {
				return false
			}
			groups := row["groups"]
			return !strings.Contains(groups, "P:mg01") && strings.Contains(groups, "P:mg02")
		}, 60*time.Second, 2*time.Second, "publisher should be unpublished from mg01 but still publishing to mg02")

		// Tunnels should still be up — key assertion: no disconnect required.
		err := publisherClient.WaitForTunnelUp(t.Context(), 30*time.Second)
		require.NoError(t, err, "publisher tunnel should remain up after unpublish")
		err = subscriberClient.WaitForTunnelUp(t.Context(), 30*time.Second)
		require.NoError(t, err, "subscriber tunnel should remain up after unsubscribe")

		// Restore pre-test state so the disconnect sub-test exercises the same two-group
		// teardown it did before.
		tdn.SubscribeMulticastGroup(t, subscriberClient, "mg01")
		tdn.PublishMulticastGroup(t, publisherClient, "mg01")

		require.Eventually(t, func() bool {
			pub := publisherRow()
			sub := subscriberRow()
			if pub == nil || sub == nil {
				return false
			}
			return strings.Contains(pub["groups"], "P:mg01") &&
				strings.Contains(pub["groups"], "P:mg02") &&
				strings.Contains(sub["groups"], "S:mg01") &&
				strings.Contains(sub["groups"], "S:mg02")
		}, 60*time.Second, 2*time.Second, "roles should be restored for both clients")
	}) {
		t.Fail()
		return
	}
```

- [ ] **Step 2: Run the e2e test**

Run (requires sudo on Linux for libpcap; see CLAUDE.md):

```bash
make e2e-test RUN=TestE2E_Multicast
```

Expected: the new `modify_roles` sub-test passes. The existing `connect` and `disconnect` sub-tests still pass unchanged.

If the test fails on the `multicast_pubs` / `multicast_subs` column names, fix per the note above and re-run.

- [ ] **Step 3: Run Go lint and formatter**

Run:

```bash
make go-lint && make go-fmt
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add e2e/multicast_test.go
git commit -m "e2e: verify multicast role modification without disconnect"
```

---

### Task 10: Final verification across the whole workspace

- [ ] **Step 1: Full Rust test suite**

Run:

```bash
make rust-test
```

Expected: all tests pass.

- [ ] **Step 2: Full Rust lint**

Run:

```bash
make rust-lint
```

Expected: no warnings.

- [ ] **Step 3: Full Go lint and build**

Run:

```bash
make go-lint
```

Expected: no warnings.

- [ ] **Step 4: Manual devnet sanity check (optional, documented)**

On a local devnet:

```bash
dev/dzctl destroy -y && dev/dzctl build   # only if no devnet running
# Connect a multicast user to two groups via connect:
docker exec dz-local-client-<pubkey> doublezero connect Multicast --subscribe mg01 mg02

# Remove one via the new verb:
docker exec dz-local-client-<pubkey> doublezero multicast unsubscribe mg01

# Verify onchain state:
docker exec dz-local-manager doublezero user list
# → the user should show subscribers = [mg02] only, and the tunnel should still be up.

# Verify daemon status:
docker exec dz-local-client-<pubkey> doublezero status
# → session_status should remain "established".
```

---

## Known limitation (spec-documented)

`doublezero multicast unpublish <last-publisher-group>` in legacy-allocation environments will still cause a brief service reprovision because the smartcontract sets `UserStatus::Updating` and the daemon's reconciler tears the service down. The CLI warns when this would happen. Environments using onchain allocation are unaffected. This is intentionally out of scope for this plan.
