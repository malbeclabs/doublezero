---
applyTo: "smartcontract/programs/**"
description: "Review rules for the onchain serviceability and geolocation programs"
---

# Onchain programs

## Authorization and account safety

- Verify the owner of every PDA account read, including accounts owned by another program — a
  processor that skips the check can be handed an account owned by someone else. For accounts from
  the serviceability program, verify the owner is the serviceability program id; for a program's own
  PDAs, verify the owner is `program_id`. Singleton PDAs (`ProgramConfig`, `GlobalState`) are the
  exception: the discriminator check via `try_from` is sufficient because only one valid account
  exists.
- `authorize()` should run immediately after `GlobalState` is parsed, before argument validation,
  PDA derivation, and `data_is_empty` probes, so an unauthorized caller cannot probe for account
  existence or trip validation errors before being denied. The accepted exception is a cheap
  all-`None` no-op rejection placed above it.
- Optional trailing accounts must be disambiguated by PDA match (`split_trailing_permission` in
  `authorize.rs`), never by position. The on-wire tail is
  `[...command accounts, payer, system, permission?]`. The Rust SDK's Permission append is currently
  deferred — `build_with_permission` in `crates/doublezero-serviceability-instruction/src/common.rs`
  delegates to `build`, and `SDK_ATTACHES_PERMISSION_ACCOUNTS` in
  `smartcontract/cli/src/permission/audit.rs` is `false` — so a trailing Permission arrives only from
  external clients today, and the processor must still handle both shapes.
- Flag any change that broadens who may call an instruction — a legacy-authority mapping that adds
  the sentinel, the activator, or a new flag — even when the broadening is plausibly intended. Say
  which authorities gain access and ask for it to be confirmed and documented.
- A guard keyed on a legacy `GlobalState` field (`feed_authority_pk`, allowlists) must carry a
  comment recording that it silently no-ops once that authority moves to a Permission account. The
  same applies to a test that would then assert the regression as an expected success.
- Migrating an instruction to `authorize()` means updating `AUTHORIZE_GATED_FLAGS`,
  `legacy_keys_for_flags`, and `check_legacy_any` together in
  `smartcontract/programs/doublezero-serviceability/src/authorize.rs` (a `#[cfg(test)]` test asserts
  the last two agree), and revisiting `NON_MIGRATED_SUBSYSTEMS` in
  `smartcontract/cli/src/permission/audit.rs`. A gated flag missing from `AUTHORIZE_GATED_FLAGS`
  makes `doublezero permission audit` understate lockout risk.

## Instruction validation

- Reject duplicate keys and degenerate input — a repeated key that double-counts a reference count,
  an empty required vec, a cap set below the current count — instead of silently deduping or
  resolving to the first match. A reference count that is never decremented cannot be reclaimed.
- Bound every caller-supplied `Vec` and `String`. Unbounded input bloats the account, charges rent
  to the payer, approaches account-size limits, and turns O(n²) validation into a cost attack.
- Use checked or saturating arithmetic on borsh-decoded values. The workspace sets no
  `overflow-checks` profile, so debug and test builds panic where the deployed SBF release build
  wraps.
- Reject an all-`None` update payload before the account is re-serialized. Check the fields
  explicitly — destructure the args struct so adding a field is a compile error — rather than
  comparing against `Default::default()`.
- Do not derive `Default` on instruction args structs, and construct them with explicit field
  literals rather than `..Default::default()`, so adding a field forces the caller to take a
  position instead of silently defaulting.

## Wire compatibility

- Changing the type, order, or presence of a field in a borsh-serialized account or instruction-args
  struct breaks deployed clients and stored accounts, and shifts `BorshDeserializeIncremental`
  offsets. Require an append-only or in-place-compatible layout, evidence that no live account
  carries the old shape, a `### Breaking` CHANGELOG entry with a deploy note, and round-trip
  coverage for the all-set, all-`None`, and truncated payloads.
- A state or args enum/struct change must keep `make generate-fixtures` compiling — the generator
  crates are workspace-excluded, so the normal build and test targets skip them — and the new or
  changed field must be asserted in the Go, Python, and TypeScript decoders against the shared
  fixture.
- Format changes must respect the compatibility window (RFC-10): every version in
  `[min_compatible_version, program_version]` must be able to read state written by any other
  version in it. So a change that introduces a new format reads both formats and keeps *writing*
  the old one; writing the new format only begins in a release that has raised
  `min_compatible_version` past every reader of the old one, and the old-format read path is
  removed in a later release once the window has closed. A diff that starts writing a new format in
  the same release that adds it is a break for every client already in the window.
- Recheck account-size and rent constants whenever a create path can write more than the comment
  above the constant assumes.

## State and errors

- Give each rejection path its own error variant when a test needs to distinguish it. Several
  branches sharing `InvalidArgument` makes every negative test unable to pin which one fired.
- Flag masks should clear the bits the instruction owns and preserve everything else
  (`flags = (flags & !OWNED) | new`) rather than whitelisting the bits to preserve, or the next flag
  added is silently clobbered. Validate a permission bitfield against an `ALL_FLAGS` mask so a value
  built only from undefined bits cannot pass a `!= 0` check.
- Any processor that allocates from or deallocates against a resource pool (`SegmentRoutingIds`,
  `TunnelIds`, `LinkIds`, `UserTunnelBlock`, `DeviceTunnelBlock`, `DzPrefixBlock`,
  `MulticastGroupBlock`, `MulticastPublisherBlock`) must update the matching `verify_*` function in
  `smartcontract/cli/src/resource/verify.rs`, plus that module's tests. Otherwise the verifier
  reports the allocation as `AllocatedButNotUsed`, or misses a leak when a deallocation is dropped.
