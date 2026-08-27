# Multicast: modify subscriptions and publishers without disconnecting

**Status:** Design approved, pending implementation plan.
**Date:** 2026-04-23.

## Problem

A connected multicast user cannot modify their set of publisher or subscriber group memberships without running `doublezero disconnect` (which deletes the user account) and then reconnecting with the new desired set.

Today, `doublezero connect Multicast --publish <groups> --subscribe <groups>` handles first-time connection and is purely additive — it will add new groups to an existing connected user, but it never removes. There is no CLI path to drop a subscriber role, drop a publisher role, or otherwise narrow the active role set. Users work around this by disconnecting and reconnecting.

## Scope

In scope:

- Add four new CLI subcommands under `doublezero multicast` so users can add or remove publisher/subscriber roles on an already-connected multicast user without running `disconnect`.
- Extend the existing multicast e2e suite to cover the new commands.

Out of scope:

- Smartcontract changes. The existing `UpdateMulticastGroupRoles` instruction already supports add and remove via its `publisher` / `subscriber` boolean flags, and is what `connect` uses today.
- Daemon changes. In particular, the legacy-allocation codepath in the smartcontract transitions a user's status to `Updating` when they gain their first publisher or lose their last publisher, and the daemon's reconciler treats `Updating` as "not provisioned" and tears the service down. This teardown behavior is a known limitation and is not fixed here. Environments using onchain allocation already avoid `Updating` for these transitions.
- Making `connect` declarative / non-additive. `connect` stays as-is.

## Commands

All four commands live under the existing `doublezero multicast` namespace (which currently only exposes administrative `group` subcommands). Each takes one or more group codes.

```
doublezero multicast subscribe   <group> [<group> ...]   # add subscriber role(s)
doublezero multicast unsubscribe <group> [<group> ...]   # remove subscriber role(s)
doublezero multicast publish     <group> [<group> ...]   # add publisher role(s)
doublezero multicast unpublish   <group> [<group> ...]   # remove publisher role(s)
```

`subscribe` and `publish` overlap functionally with `doublezero connect Multicast --subscribe/--publish` when a user is already connected. They are added for interface symmetry with the `un-` variants and to offer a shorter invocation for the common "already connected, adjust my roles" case.

## Behavior

**Precondition.** A Multicast user must already exist for the caller's `client_ip`. If none, the command fails with:

```
No active multicast user for <ip>. Run 'doublezero connect Multicast --publish/--subscribe <group>' first.
```

These commands never create users — that remains the responsibility of `connect`.

**Group code resolution.** Group codes are resolved to pubkeys via `ListMulticastGroupCommand`, matching the pattern used by `connect::execute_multicast`. Any unknown code fails the whole command before any onchain call is issued.

**Onchain call.** For each resolved group, one `UpdateMulticastGroupRolesCommand` is issued. The onchain instruction is a per-flag "set" (passing `publisher: false` removes the publisher role for that group; passing `publisher: true` adds it), so each verb carries the *other* role's current value through unchanged to avoid clobbering it:

| Verb          | `publisher` flag               | `subscriber` flag              |
| ------------- | ------------------------------ | ------------------------------ |
| `subscribe`   | `user.publishers.contains(g)`  | `true`                         |
| `unsubscribe` | `user.publishers.contains(g)`  | `false`                        |
| `publish`     | `true`                         | `user.subscribers.contains(g)` |
| `unpublish`   | `false`                        | `user.subscribers.contains(g)` |

**No-op handling.** If a group is already in the requested state for the given verb (e.g. `subscribe` on a group the user is already subscribed to, or `unpublish` on a group the user does not publish to), the CLI logs a skip message (`already subscribed to <code>`, `not publishing to <code>`, etc.) and moves on without issuing an onchain call for that group. The command as a whole still succeeds.

**Last-publisher unpublish warning.** When an `unpublish` would empty `user.publishers`, the CLI prints a warning that the service may briefly reprovision — this is the legacy-allocation limitation described above. The command proceeds. No interactive prompt; the warning is informational.

**Daemon reconciliation.** No explicit daemon notify. The daemon's reconciler polls onchain state and will add or drop multicast routes automatically. The CLI prints `Updated. Routes will adjust shortly.` and exits.

## Implementation layout

### New and modified files

- **`client/doublezero/src/cli/multicast.rs`** — extend `MulticastCommands` with four new variants: `Subscribe`, `Unsubscribe`, `Publish`, `Unpublish`. Each takes an args struct with one or more group codes. The existing `Group(MulticastGroupCliCommand)` variant is unchanged.
- **`client/doublezero/src/command/multicast.rs`** *(new)* — one handler per verb, sharing helpers:
  - `resolve_groups(client, codes) -> Vec<Pubkey>` — mirrors the group-code resolution in `connect::execute_multicast`.
  - `load_multicast_user(client, client_ip) -> (Pubkey, User)` — finds the single Multicast user for the caller's `client_ip`, returning the precondition error when absent.
  - `apply_role_change(client, user_pk, user, group_pk, publisher, subscriber)` — builds the `UpdateMulticastGroupRolesCommand` with the correct carry-through of the opposite role, handles the no-op skip log, emits the last-publisher warning when applicable.
  - Each of the four verb handlers: resolve groups, load user, iterate groups calling `apply_role_change` with the flag pattern from the table above, print the completion message.
- **`client/doublezero/src/main.rs`** — dispatch the four new `MulticastCommands` variants to the new handlers, following the existing `Command::Connect(args) => args.execute(&client).await` pattern.

No SDK changes — `UpdateMulticastGroupRolesCommand` already exists and is what `connect` invokes for additive role changes today.

Handlers live in `client/doublezero/src/command/` (not `smartcontract/cli/src/`) because they depend on client-side `client_ip` discovery and user lookup, matching where `connect.rs` and `disconnect.rs` already sit. The administrative `multicast group *` commands remain in the smartcontract CLI crate.

## Testing

### Unit tests (Rust)

Colocated with the new command handlers in `client/doublezero/src/command/multicast.rs` (or a dedicated test module), using the same mocking patterns as the existing `connect` tests:

- Happy-path for each verb: resolves groups, calls `UpdateMulticastGroupRolesCommand` with the correct flag pair (including correct carry-through of the opposite role), succeeds.
- Missing multicast user → precondition error with the documented message.
- Unknown group code → error before any onchain call is issued.
- No-op: `subscribe` on a group the user is already subscribed to skips; same for `publish`. `unsubscribe` and `unpublish` on groups the user is not in also skip.
- Role carry-through: `unsubscribe` on a group where the user is *also* a publisher keeps the publisher role (verify the outgoing `UpdateMulticastGroupRolesCommand` has `publisher: true, subscriber: false`).
- Last-publisher `unpublish`: warning is emitted and the command still proceeds.

### E2E test

Extend the existing multicast e2e suite in `e2e/` (e.g. `multicast_test.go` or a new sibling file under `e2e/internal/qa/client_multicast.go`) to exercise the full flow end-to-end. At minimum:

1. Connect a client with `--subscribe groupA`.
2. Run `multicast subscribe groupB`; assert the onchain user now has `subscribers = [A, B]` and the client has multicast routes for both groups.
3. Run `multicast unsubscribe groupA`; assert `subscribers = [B]` and routes for A are gone.
4. Run `multicast publish groupC`; assert publisher role is added (onchain allocation path; no teardown).
5. Run `multicast unpublish groupC`; assert publisher role is removed. Note the expected `Updating` teardown in legacy-allocation environments and confirm the onchain-allocation path does not tear down.

Mirror whichever helpers in `e2e/internal/qa/client_multicast.go` are most convenient (e.g. add `SubscribeMulticast`, `UnsubscribeMulticast`, etc. alongside the existing `ConnectUserMulticast_*` helpers).

### Manual local-devnet verification

Document in the PR's Testing Verification section: on `dev/dzctl` local devnet, connect a client with `--subscribe groupA`, then exercise each new verb and inspect state with `doublezero user list` (onchain) and `doublezero status` (daemon) to confirm the expected transitions.

## Known limitation (documented)

In legacy-allocation environments, running `doublezero multicast unpublish <last-publisher-group>` still causes a brief service reprovision because the smartcontract transitions the user to `Updating` and the daemon's reconciler tears the service down. Onchain-allocation environments already avoid this. Fixing this requires either daemon or smartcontract changes and is deliberately out of scope for this work.
