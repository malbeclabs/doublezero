---
applyTo: "smartcontract/sdk/rs/**,smartcontract/cli/**,crates/**,client/doublezero/**,config/src/**"
description: "Review rules for the Rust SDK, instruction builders, and the doublezero CLI"
---

# Rust SDK, instruction builders, and CLI

## Transaction semantics

- A refactor must not change transaction semantics as a side effect. Compute-budget and heap-frame
  prepends, `skip_preflight`, structured simulation-error extraction, and landed-transaction log
  recovery apply uniformly to every send path. A new send helper that skips any of them is a defect
  even when the tests still pass.
- Cache invalidation must fire on both the `Ok` and the `Err` arm. A send can return `Err` for
  reasons that do not mean the transaction failed (confirmation timeout, blockhash-expiry
  ambiguity, a dropped connection after send), so a landed-but-`Err` create or delete otherwise
  leaves a stale memo behind.

## Instruction builders

- Builders are infallible by contract. The bound check on caller-supplied input
  (`u8::try_from(n).map_err(...)`) belongs in the command, so oversized input stays an error instead
  of becoming a release-build panic — do not drop such a guard while migrating a command to a
  builder. Where a builder must handle an unreachable overflow itself, panic with `expect` rather
  than clamping with `unwrap_or(u8::MAX)`: emitting a count that disagrees with the account list is
  worse than a panic.
- A builder must force the flags its processor hard-rejects (`use_onchain_allocation`,
  `use_onchain_deallocation`), so default args cannot produce an always-reverting instruction. When
  a field is derived and any caller value ignored, say so in the doc comment.
- A value must have exactly one source. When it appears both as an account parameter and inside the
  args with nothing binding the two, keep the args field — it is on the wire — and derive the
  `AccountMeta` from it.
- Doc comments in the builder crate are the anti-drift spec: a sentence that contradicts the
  processor invites a future maintainer to "fix" working code. Verify each claim about writability,
  Permission appends, and account order against the processor.
- Batch-size and headroom comments must name the real limits — the per-transaction account-lock
  limit is 64, and the binding constraint as batches grow is the ~1232-byte packet size. Flag any
  "Solana caps transactions at 32 accounts" claim, and any restatement of a batch constant's
  arithmetic outside the module that owns the constant.

## CLI

- Resolving a human-readable code to a pubkey must refuse when the code is ambiguous rather than
  returning an arbitrary match — most importantly for `update` and `delete`, where the wrong match
  mutates or destroys the wrong account.
- Do not collapse distinct failures into one message. `.map_err(|_| eyre!("Feed not found"))` hides
  the disambiguation hint the underlying error carries; preserve the source error.
- A change to an exit code or output shape is a contract change for the scripts and services that
  parse it — require an enumeration of those consumers and how each behaves afterwards.
- New verbs and module crates follow RFC-20 (`rfcs/rfc20-cli-standardization.md`, summarized in
  `docs/cli-standard.md`): shared validators and formatters from `doublezero-cli-core`, diagnostics
  through `tracing`, and no redeclaring the global flags the `doublezero` binary owns.
