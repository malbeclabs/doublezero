# PR Split Strategy: SetUserBGPStatus

## Overview

The change adds `BGPStatus`, `last_bgp_up_at`, and `last_bgp_reported_at` to the `User` account, a new `SetUserBGPStatus` instruction (variant 102), and propagates the new field across Rust SDK, CLI, Go SDK, Python SDK, and TypeScript SDK.

Split into 3 PRs for independent review. Each builds on the previous.

---

## PR 1 — `smartcontract: add BGPStatus fields and SetUserBGPStatus instruction`

Core onchain change. Reviewable without any SDK knowledge.

**Files:**
- `smartcontract/programs/doublezero-serviceability/src/state/user.rs` — `BGPStatus` enum + 3 new fields on `User`
- `smartcontract/programs/doublezero-serviceability/src/processors/user/setbgpstatus.rs` *(new)* — instruction processor
- `smartcontract/programs/doublezero-serviceability/src/processors/user/mod.rs` — register module
- `smartcontract/programs/doublezero-serviceability/src/instructions.rs` — variant 102
- `smartcontract/programs/doublezero-serviceability/src/entrypoint.rs` — dispatch arm
- `smartcontract/programs/doublezero-serviceability/src/processors/accesspass/set.rs` — fix test `User` structs + updated size constant
- `smartcontract/programs/doublezero-serviceability/src/processors/user/create_core.rs` — new fields in `User` initializer
- `smartcontract/programs/doublezero-serviceability/src/processors/user/closeaccount.rs` — new fields in test `User` initializer
- `smartcontract/programs/doublezero-serviceability/tests/user_tests.rs` — `test_set_user_bgp_status`

**Authorization:** `device.metrics_publisher_pk == payer` OR `foundation_allowlist.contains(payer)`. Requires `device` account as input.

**Slots:** `last_bgp_reported_at` always updated; `last_bgp_up_at` only updated when status transitions to `Up`.

**Verification:**
```bash
cargo test -p doublezero-serviceability test_set_user_bgp_status
make rust-lint
```

---

## PR 2 — `smartcontract: expose BGPStatus in SDK and add bgp_status column to user list`

Rust SDK re-export + CLI display + fix all test `User` struct initializers in the Rust workspace.

**Depends on:** PR 1 merged.

**Files:**
- `smartcontract/sdk/rs/src/lib.rs` — re-export `BGPStatus`
- `smartcontract/cli/src/user/list.rs` — `UserDisplay.bgp_status` field + test updates
- `smartcontract/sdk/rs/src/commands/**` — fix test `User` structs (13 locations)
- `smartcontract/cli/src/**` — fix test `User` structs (8 locations)
- `client/doublezero/src/command/connect.rs` — fix test `User` struct
- `activator/src/process/user.rs` — fix test `User` structs (16 locations)

**Verification:**
```bash
make rust-lint
cargo test -p doublezero_sdk
cargo test -p doublezero_cli
```

---

## PR 3 — `sdk: deserialize BGPStatus in Go, Python, and TypeScript SDKs`

Add BGPStatus deserialization to the three multi-language SDKs. Fully backwards compatible (defensive readers with `unwrap_or_default`).

**Can be opened in parallel with PR 2.**

**Files:**
- `sdk/serviceability/go/state.go` — `BGPStatus` type + constants + `String()`/`MarshalJSON()`, add `TunnelEndpoint` + `BGPStatus`/`LastBGPUpAt`/`LastBGPReportedAt` to `User` struct
- `sdk/serviceability/go/deserialize.go` — add reads at end of `DeserializeUser`
- `sdk/serviceability/python/serviceability/state.py` — `BGPStatus(IntEnum)` + fields on `User` dataclass + reads in `from_bytes`
- `sdk/serviceability/typescript/serviceability/state.ts` — `bgpStatus`/`lastBGPUpAt`/`lastBGPReportedAt` fields on `User` interface + reads in `deserializeUser` + `bgpStatusString` helper

**Verification:**
```bash
make sdk-test
make generate-fixtures  # regenerate .bin/.json fixtures if needed
```
