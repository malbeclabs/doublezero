# RFC-28: Declarative Access Pass Reconciliation

## Summary

**Status: `Draft`**

Two commands, `doublezero access-pass plan` and `doublezero access-pass apply`, reconcile access passes against a YAML document that describes the access a fleet should have. The document names, per pass, the multicast groups it may publish to and subscribe to and the tenant granting it IBRL (unicast) access. `plan` reports the difference between the document and the ledger and writes nothing; `apply` sends that same difference after a confirmation.

The result is that a fleet's access is described in one reviewable file, the difference between intent and reality is visible before anything is written, and a converged document is a no-op that configuration management can run on every pass.

## Motivation

Access is currently granted one instruction at a time. Adding a group to a host is:

```
doublezero multicast group allowlist subscriber add --code <group> --user-payer <pk> --client-ip <ip>
```

That is one invocation per (group x pass x role). Six servers across four groups in both roles is forty-eight commands, and the IBRL half is a separate `access-pass set` on top. Three problems follow.

**There is no way to see the whole picture.** Answering "which groups does 203.0.113.10 have?" means running `multicast group allowlist ... list --code X` once per group and reading the output by eye, because the per-group listing is the only view and it scans every access pass each time. The question an operator actually asks is a grid — hosts against groups — and no command produces one.

**There is no way to see what a change would do.** Every verb writes immediately. An operator who wants to know whether a host is already correct has to infer it from listings, and an operator who wants to remove access has no way to preview the removals.

**The state is not described anywhere.** The intended access lives in whoever last ran the commands. Two hosts that should be identical drift, and nothing detects it. A configuration-management driver can be written on top of the individual verbs — one exists — but it has to compute the diff itself in the templating language, which is where it is hardest to test.

The failure this produces is quiet rather than loud: a grant that was never issued looks exactly like one that was, until the traffic does not arrive.

## New Terminology

- **Definition document**: the YAML file describing the access passes and the access each should have. The desired state.
- **Plan**: the set of writes that would bring the ledger to the state the document describes, together with the declared grants already satisfied and the entries the tool refuses to act on.
- **Declarative field**: a field whose absence is meaningful. A group the document does not name is revoked; an entry with no `ibrl` has its tenant cleared.
- **Satisfied grant**: a grant the document declares that the pass's allowlist already provides.
- **Blocked entry**: a declared pass the plan refuses to act on, with the reason.
- **IBRL grant**: unicast access, comprising the pass's tenant and its `last_access_epoch`, which are written together.

## Alternatives Considered

1. **Do nothing.** Keep the per-instruction verbs. Rejected: it leaves the three problems above, and it pushes the diff into whatever driver sits on top, where it is untested.

2. **Bulk flags on the existing verbs** — repeated `--code` / `--client-ip` / `--user-payer` forming a cross product. This shortens the forty-eight commands to two and is worth doing on its own merits, but it grants only. It cannot revoke what is no longer wanted, cannot report drift, and still describes nothing. Complementary rather than an alternative.

3. **A generic reconciler across every resource type** — `doublezero plan` / `apply` over a document covering devices, links, locations, tenants and passes, with a provider per type. Rejected for now as disproportionate: it needs a resource/action boundary, a dependency ordering between types, and a much larger schema to agree on, for one resource's worth of demonstrated need. The design here does not preclude it; `access-pass plan` can be promoted to `plan --target access_pass` if a second resource earns it.

4. **Solve it entirely in configuration management** — a native Ansible module, with the desired state expressed as playbook tasks and no file format at all. This is attractive when Ansible is the only driver: it gets check mode, diff and inventory looping for free. Rejected as the primary form because it makes the capability unavailable to anyone not running Ansible, leaves no single artifact describing intended state, and still needs the CLI to expose the diff — so it moves the work rather than removing it. A module wrapping these commands remains a reasonable addition later.

5. **A Terraform provider.** Rejected: it introduces state files and a second source of truth for accounts that are already authoritative onchain, and the plan/apply ergonomics can be had without either.

6. **TOML rather than YAML.** Rejected: `serde_yaml` is already a workspace dependency and already used by `export`, configuration management is YAML-native, and adding `toml` buys nothing.

## Detailed Design

### The document

```yaml
defaults:
  user_payer: 3UrShLQz2Y9UEaz69QhbZ41px91JYFSWd4hEs33ag3se

access_passes:
  - client_ip: 203.0.113.10
    multicast:
      publish:
        - mg-marketdata-tob
        - mg-marketdata-mbp

  - client_ip: 203.0.113.12
    user_payer: AB3gAfgVBtb3AoJ2GwRGCuzCSWXit4isKLYm3kULWuf7
    ibrl: solana
    multicast:
      subscribe:
        - mg-analytics-mbp

  - client_ip: 203.0.113.13
    ibrl: solana
```

| Field | Scope | Meaning |
| --- | --- | --- |
| `defaults.user_payer` | document | Applied to any entry omitting `user_payer`. |
| `client_ip` | entry, required | With `user_payer`, the pass's PDA seeds. |
| `user_payer` | entry, optional | Overrides the default. Accepts `me`. |
| `ibrl` | entry, optional | Tenant code granting unicast access. One code. |
| `multicast.publish` | entry, optional | Group codes this pass may publish to. |
| `multicast.subscribe` | entry, optional | Group codes this pass may subscribe to. |

The schema deliberately matches the per-host declaration an operator already writes in configuration-management inventory, so both describe a host the same way.

**Every field is declarative.** A group the document does not name is revoked from that pass; an entry with no `ibrl` has its tenant cleared; an entry with no `multicast` block declares no groups and therefore revokes all of them. This is what makes the document a description of state rather than a list of additions, and it is the reason `plan` exists — the revocations are the half worth reviewing.

`ibrl` is a scalar because a pass admits one tenant and `SetAccessPass` is the only instruction that writes `tenant_allowlist`; setting it is inherently a replace, so a list would mislead.

**Unknown keys are rejected.** Every optional field is read with a default, so `subscibe:` would otherwise parse as valid YAML, contribute nothing, and leave the host quietly unsubscribed. `deny_unknown_fields` on every type makes a misspelling visible:

```console
Error: invalid access-pass document: access_passes[2]: unknown field `ibrll`,
expected one of `client_ip`, `user_payer`, `ibrl`, `multicast` at line 27 column 5
```

Unknown group and tenant codes are rejected the same way, and all of them are named at once, because the program resolves a code to a PDA without checking that anything is behind it — an unchecked typo writes a grant pointing at nothing.

The payer is resolved after parsing rather than during it, so a document can be validated with no keypair and no network.

### Diffing

One document entry produces up to two kinds of change.

**Multicast.** For each role, the declared set is compared against the pass's `mgroup_pub_allowlist` / `mgroup_sub_allowlist`. Missing entries become grants, undeclared entries become revokes.

A declared subscribe is written to the allowlist even when a feed on the pass carries the group. Feed coverage is reported as a warning, never acted on, because it is narrower than it looks: it authorizes a join only at connect (`CreateSubscribeUser`), and there only in the feed's own metro (`check_feed_metro_coverage` rejects a mismatch with `MetroMismatch`). `doublezero multicast subscribe` on an existing user sends `UpdateMulticastGroupRoles`, which has no feed path and checks the subscriber allowlist for every role gained, whatever the pass type. Suppressing the write therefore left the host refused at subscribe time while the document reported itself converged — a silent, late failure of exactly the kind the epoch pinning exists to prevent. Writing the entry costs one transaction the first time and nothing after, so idempotency is preserved by the allowlist itself rather than by the feed rule.

An allowlist key that resolves to no group is skipped rather than revoked. It cannot be named, so the document cannot declare it, and a plan line printing a bare pubkey would be unreadable. See *Known limitation* below.

**IBRL.** The declared tenant is compared against the pass's first `tenant_allowlist` entry, and the tenant and `last_access_epoch` are treated as one grant. A declared `ibrl` requires the epoch to be unlimited: the epoch gates unicast user creation only, any finite value turns a later `connect ibrl` on that IP into a failure at an unpredictable date, and `0` is not "expired" but "no epoch defined", which blocks every unicast type outright. Either half drifting re-sends the same `SetAccessPass`.

Clearing a tenant re-sends the stored epoch, with one exception: `SetAccessPassCommand` rejects a finite epoch that has already elapsed, which would otherwise make IBRL impossible to revoke from an expired pass — the run would fail on a value the operator never mentioned, on a write whose purpose is to take a grant away. An elapsed epoch is therefore sent as `0`, which blocks every unicast type and so matches what removing the tenant intends. An epoch still in the future is preserved, since zeroing it would revoke more than the document asked for.

### Reads

The multicast groups are one scan for the whole document. Tenants are read when the document declares an `ibrl`, and otherwise only once a pass turns out to carry one — a pass with a tenant still has to be cleared, so the scan cannot be skipped merely because the document is silent. Feeds are read only if some pass carries a feed seat. Access passes are fetched per entry, because each is a distinct PDA.

### Writing

Multicast changes use the four existing allowlist instructions, one transaction each; there is no multi-group allowlist instruction.

The IBRL change uses `SetAccessPass`, which overwrites `accesspass_type`, `last_access_epoch`, `allow_multiple_ip` and both seat caps from its arguments. So the write reads the pass first and re-sends those unchanged: the transaction moves the tenant, pins the epoch, and touches nothing else. (`mgroup_*_allowlist` and `DZF_LOCKED` survive a `set` untouched, and the program preserves EdgeSeat feed seats when both the stored and incoming types are EdgeSeat.)

**Writes target the stored pass, not the declared IP.** A pass with `allow_multiple_ip` is stored at the PDA seeded with `0.0.0.0` and serves any client IP, and access-pass resolution prefers it over an exact-IP pass. `SetAccessPass` seeds its PDA from the `client_ip` argument, so sending the declared address would write a different account than the one the plan described. The plan warns whenever a declared address resolves to a shared pass, because every group granted there is granted to every host using it.

### Output

```console
$ doublezero access-pass plan -f access-passes.yml
DoubleZero will perform the following actions:

  # access_pass 203.0.113.10 / 3UrS…g3se   (4Fq…2Lm)
  + subscriber  mg-analytics-mbp
  - publisher   mg-marketdata-legacy

IBRL (unicast) access:

  # access_pass 203.0.113.12 / AB3g…Wuf7   (8Bd…9Wp)
  + ibrl        solana
  ~ epoch       -> unlimited

Plan: 1 to add, 1 to remove, 1 IBRL change(s), 3 satisfied, 0 blocked.
```

`+` grants, `-` revokes, `~` changes in place. `--verbose` lists the satisfied grants; `--json` emits the same plan as a machine-readable object.

`apply` prints the same plan, prompts, sends each change, and reports the outcome per item. Each change is its own transaction, so a run continues past a failure and names what did and did not land, then exits non-zero.

### Blocked entries

Two situations are reported and refused rather than attempted. Both verbs exit non-zero when there are any, and `apply` refuses the whole run before the prompt rather than converging the subset it can, so the exit code carries one meaning. `--allow-blocked` opts into the subset deliberately, and still exits non-zero.

**A pass that does not exist.** The document does not create access passes. `AddMulticastGroup*Allowlist` against an empty PDA silently creates a `Prepaid` pass with one unicast seat, one multicast seat and `last_access_epoch: 0`, so a typo'd address would mint a junk pass that looks real. The operator creates the pass with `access-pass set` first.

**Two entries resolving to one pass and disagreeing.** A pass with `allow_multiple_ip` lives at the `0.0.0.0` PDA and serves any client IP, so two declared addresses can resolve to the same account. The allowlists live on that one account, so entries declaring different groups are not two grants but one contradiction: each plans a revoke of what the other asked for, and apply flips the state on every run without converging. Entries that agree are redundant rather than contradictory and are planned once.

An earlier draft also refused a group leaving both allowlists at once, on the theory that the host's detach verbs send the role being *kept* as desired state and would then be unauthorized. That is no longer so: #4302 changed `check_mgroup_allowlists` to authorize only the roles a user is *gaining*, so a role already held is not re-checked and the detach is a legal write. The refusal was blocking a document that works, and has been removed.

### Automation

`apply --json` writes exactly one JSON object to stdout, with `changed` first:

```yaml
- ansible.builtin.command:
    argv: [doublezero, access-pass, apply, --file=/etc/doublezero/access-passes.yml,
           --auto-approve, --json]
  register: dz
  changed_when: (dz.stdout | from_json).changed
```

`--json` requires `--auto-approve` (or `--dry-run`), because there is no terminal to answer the confirmation prompt on and the alternative is hanging on a read that never returns.

A converged document is a no-op: the second `apply` reports `changed: false` and sends nothing. The property rests on the allowlist comparison alone, so it does not depend on any rule about what a feed already covers.

## Impact

**Codebase.** Three new modules under `smartcontract/cli/src/accesspass/` — the document, the plan engine and renderer, and the apply verb — plus two subcommand arms. No new dependency: `serde_yaml` is already in the crate. No program change.

**Operations.** A fleet's access becomes a file that can be reviewed, diffed and version-controlled. The intended use is that the file is templated from configuration-management inventory, where the addresses already live, so the document is generated rather than hand-maintained.

**Performance.** A run costs one multicast-group scan, at most one tenant scan, at most one feed scan, and one account fetch per declared pass — against the current cost of one full access-pass scan per group listed. Writes are unchanged in number and kind.

**Existing commands.** Untouched. The per-instruction verbs remain, and are still the right tool for a one-off.

## Security Considerations

The commands write only through existing instructions and add no privilege. Authorization is enforced onchain in every case; the plan is a client-side view and is never a substitute for the program's checks.

Granting publish requires `MULTICAST_ADMIN`; granting subscribe and changing the tenant require `ACCESS_PASS_ADMIN`. A signer holding one but not the other sees that half of a run succeed and the other half fail, per item. That is reported rather than hidden, so a partially applied run is legible.

The declarative semantics are the main new hazard: a document that omits a group revokes it, and a document that omits `ibrl` clears a tenant, which can remove a live host's unicast access. Three things bound it — `plan` writes nothing and shows the revocations, `apply` prints the same plan and prompts before sending, and `--auto-approve` has to be passed deliberately. An operator adopting an existing fleet should write the document to describe reality and confirm the first `plan` is empty before changing anything.

The read-modify-write on `SetAccessPass` carries a small race: another writer can change a preserved field between the read and the write. The window is one round trip, and the alternative — omitting the fields — resets them unconditionally. Optional fields on `SetAccessPassArgs` would remove the race; see Open Questions.

Refusing to create a missing access pass is a deliberate safety property, not a limitation: it keeps a mistyped address from minting a pass rather than erroring.

## Backward Compatibility

Additive. Two new subcommands; no existing command, flag, output or instruction changes. The document format is new, so nothing depends on it yet — which is the moment to settle the open questions below.

## Open Questions

1. **Is `defaults` worth keeping?** It saves repeating one pubkey per entry, but `user_payer` together with `client_ip` *is* the account address, and a wrong default silently reconciles a different account for every entry that omits it. Configuration management already handles the fleet-wide case by interpolating the value per host rather than defaulting it. Removing the block and requiring `user_payer` per entry is the safer default.

2. **Should an omitted `ibrl` clear the tenant, or leave it alone?** Clearing is consistent with the multicast lists and with a document that claims to describe intended state. Leaving it alone treats an absent key as "not managed here", which is what the existing configuration-management role does, and is less destructive when a document is written by someone unaware of a pass's unicast grant. The two readings cannot both be right, and this decides how safe a partially-specified document is.

3. **Where should the verbs live?** `access-pass plan` is honest about the scope and does not claim generality. `doublezero plan` reads better and leaves room for other resources, but promises something not yet delivered. Renaming later is cheap while nothing depends on it.

4. **Should `plan` offer `--detailed-exitcode`?** Following `terraform plan` — 0 no changes, 1 error, 2 changes pending — would serve cron and CI without parsing JSON. It needs exit-code plumbing through the binary that no other verb has, and `--json`'s `changed` field already covers the driver case.

5. **Should `apply` be able to create missing access passes** behind an explicit flag, rather than blocking? It would make a document self-sufficient, at the cost of the safety property in Security Considerations, and it needs the pass type and caps to be declarable — which widens the schema considerably.

6. **Optional fields on `SetAccessPassArgs`.** Making the arguments `Option<T>` and having the processor write only `Some` values would let the IBRL write stop reading first, removing the race. It generalizes what the processor already does for the `ALLOW_MULTIPLE_IP` bit and for EdgeSeat feed seats, but it is a program change and is out of scope here.

## Known limitation: orphaned allowlist entries

An allowlist entry whose multicast group has since been deleted cannot be removed by anyone. `RemoveMulticastGroup*Allowlist` validates that the group account is owned by the program and deserializes it, and `DeleteMulticastGroup` closes that account — resizing it to zero and reassigning it to the system program. Once the group is gone, no call can name it. The SDK command fails earlier still, when it tries to resolve the code.

`plan` therefore skips such entries rather than proposing a revoke that would always fail, and reports nothing for them.

These entries arise from `multicast group delete`, which sweeps every access pass's allowlists before deleting the group but proceeds to delete even when that sweep reports failures — the failures surface as a warning after the group is already closed. The permission asymmetry makes it reachable in practice: a signer holding `ACCESS_PASS_ADMIN` but not `MULTICAST_ADMIN` fails every publisher removal in the sweep and then deletes the group anyway.

Two fixes are worth considering separately from this RFC. Aborting the delete when the sweep has failures would stop new orphans being created, and is a small change. Removing the ones that already exist needs a program-side instruction that drops a pubkey from a pass's allowlists without requiring the group account.
