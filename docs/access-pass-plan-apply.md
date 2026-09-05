# Declarative access passes: `plan` and `apply`

Describe the access a fleet should have — multicast publish/subscribe and IBRL (unicast) — in one
YAML document, review what would change, then apply it.

`doublezero access-pass plan` reports the difference between the document and the ledger, and
writes nothing. `doublezero access-pass apply` takes the same difference and sends it.

The design and its open questions are in [RFC-28](../rfcs/rfc28-declarative-access-passes.md).

```console
$ doublezero access-pass plan -f access-passes.yml
$ doublezero access-pass apply -f access-passes.yml
```

## The document

```yaml
defaults:
  user_payer: 3UrShLQz2Y9UEaz69QhbZ41px91JYFSWd4hEs33ag3se

access_passes:
  - client_ip: 203.0.113.10
    multicast:
      publish:
        - mg-marketdata-tob
      subscribe:
        - mg-marketdata-mbp

  - client_ip: 203.0.113.11
    user_payer: AB3gAfgVBtb3AoJ2GwRGCuzCSWXit4isKLYm3kULWuf7
    ibrl: solana
    multicast:
      subscribe:
        - mg-analytics-mbp
```

The pass is keyed by `(client_ip, user_payer)` — its PDA seeds — so those two fields name the
account each entry describes.

| Field | Scope | Notes |
| --- | --- | --- |
| `defaults.user_payer` | document | Applied to any entry that omits `user_payer`. A fleet shares one payer, so this is usually the only place it appears. |
| `client_ip` | entry, required | With `user_payer`, it is the access pass's PDA seed pair. |
| `user_payer` | entry, optional | Overrides the default. Accepts `me` for the current signer. |
| `ibrl` | entry, optional | Tenant code granting IBRL (unicast) access. One code; omit for none. |
| `multicast.publish` | entry, optional | Group codes this pass may publish to. |
| `multicast.subscribe` | entry, optional | Group codes this pass may subscribe to. |

**Every field is declarative.** A group the document does not name is revoked from that pass, and
an entry with no `ibrl` has its tenant cleared. An entry with no `multicast` block declares no
groups, and therefore revokes all of them. That is what makes the document a description of
intended state rather than a list of things to add — and it is why `plan` exists, since the
revocations are the half worth seeing before they happen.

`ibrl` is a scalar rather than a list because a pass admits one tenant, and `access-pass set` is
the only instruction that writes `tenant_allowlist` — setting it is inherently a replace.

Declaring `ibrl` also pins `last_access_epoch` to unlimited. The epoch gates unicast user creation
only, and any finite value turns a later `connect ibrl` on that IP into a failure at an
unpredictable date; `0` is not "expired" but "no epoch defined", and blocks every unicast type
outright. So the tenant and the epoch are treated as one grant: either half drifting re-sends the
same `access-pass set`.

Unknown keys are rejected. Every optional field would otherwise default silently, so `subscibe:`
would parse as valid YAML, contribute nothing, and leave the host quietly unsubscribed:

```console
$ doublezero access-pass plan -f access-passes.yml
Error: invalid access-pass document: access_passes[0].multicast: unknown field `subscibe`,
expected `publish` or `subscribe` at line 6 column 7
```

Unknown group codes are rejected too, and all of them are named at once, because the program
resolves a code to a PDA without checking that anything is there — an unchecked typo writes a
grant pointing at nothing.

## Reading a plan

```console
$ doublezero access-pass plan -f access-passes.yml
DoubleZero will perform the following actions:

  # access_pass 203.0.113.10 / 3UrS…g3se   (4Fq…2Lm)
  + subscriber  mg-analytics-mbp
  - publisher   mg-marketdata-legacy

IBRL (unicast) access:

  # access_pass 203.0.113.11 / AB3g…Wuf7   (8Bd…9Wp)
  + ibrl        solana
  ~ epoch       -> unlimited

Plan: 1 to add, 1 to remove, 1 IBRL change(s), 3 satisfied, 0 blocked.
```

`+` grants, `-` revokes, `~` changes in place. Add `--verbose` to list the grants that are already
in place, and `--json` for a machine-readable object whose `changed` field says whether anything
would move.

## What `plan` refuses to do

Blocked items are reported, and both verbs exit non-zero when there are any.

**A pass that does not exist.** The document does not create access passes. Granting an allowlist
entry against an empty PDA would silently mint a `Prepaid` pass with one unicast and one
multicast seat and no epoch, so a typo'd IP would produce a junk pass that looks real. Create the
pass first with `doublezero access-pass set`.

**A group leaving both allowlists at once.** The host's detach verbs send the role being *kept*
as desired state, and the program authorizes every `true` against these allowlists. Once both
entries are gone, `multicast unpublish` asks for `subscriber: true` and `multicast unsubscribe`
asks for `publisher: true`, and neither is allowlisted any more — the roles are stranded on the
User account with no legal write to remove them. Detach the host first
(`doublezero multicast unpublish` / `unsubscribe`), then revoke.

## What it leaves alone

- **Everything the document does not describe**: the pass type, `allow_multiple_ip`, `DZF_LOCKED`,
  the seat caps and EdgeSeat feed seats are preserved. `access-pass set` overwrites those from its
  arguments, so an IBRL change reads the pass first and re-sends them unchanged — the write moves
  the tenant, pins the epoch, and touches nothing else.
- **The pass a shared grant actually lives on.** A pass with `allow_multiple_ip` is stored at the
  `0.0.0.0` PDA and serves any client IP, so a concrete address can resolve to it. Writes target
  the stored pass rather than the declared IP, and `plan` warns when the two differ, because every
  group granted there is granted to every host using that pass.
- **Subscribe rights granted by a feed.** An EdgeSeat pass's feeds grant subscribe on their
  groups in their own metro, so a declared subscribe that a feed already covers is reported as
  satisfied and costs no transaction. Publisher is never feed-covered, so a publish gap on the
  same group is still a real gap.
- **A group that no longer exists.** An allowlist key with no group behind it cannot be named, so
  the document cannot declare it and no revoke is planned for it.

## Automation

`apply --json` writes exactly one JSON object to stdout, with `changed` first, so a
configuration-management driver can key off it:

```yaml
- ansible.builtin.command:
    argv:
      - doublezero
      - access-pass
      - apply
      - --file=/etc/doublezero/access-passes.yml
      - --auto-approve
      - --json
  register: dz
  changed_when: (dz.stdout | from_json).changed
```

`--json` requires `--auto-approve` (or `--dry-run`): there is no terminal to answer the
confirmation prompt on, so the CLI refuses rather than hanging on a read that never returns.

A converged document is a no-op — a second `apply` reports `changed: false` and sends no
transactions.

## Permissions

Granting publish requires `MULTICAST_ADMIN`; granting subscribe and changing the IBRL tenant
require `ACCESS_PASS_ADMIN`. A signer holding one but not the other will see that half of the run
succeed and the other half fail, per item, and the command exits non-zero.

## Known limitation: orphaned allowlist entries

An allowlist entry whose multicast group has since been deleted cannot be removed, and `plan`
leaves it alone rather than proposing a revoke that would fail. `RemoveMulticastGroup*Allowlist`
requires the group account, and `multicast group delete` closes it — so once the group is gone the
entry is stranded. `multicast group delete` sweeps the allowlists first, but proceeds to delete
even when that sweep reports failures, which is how such an entry arises. Removing them needs a
program-side change.
