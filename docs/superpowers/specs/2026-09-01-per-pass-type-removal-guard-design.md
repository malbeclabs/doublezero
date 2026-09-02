# Design: one close and delete instruction per access pass type

Date: 2026-09-01
Issue: [malbeclabs/infra#2470](https://github.com/malbeclabs/infra/issues/2470)
Parent: [malbeclabs/infra#2385](https://github.com/malbeclabs/infra/issues/2385)

## Problem

Serviceability has one `CloseAccessPass` instruction (variant 69) and one `DeleteUser`
instruction (variant 42). Neither handler looks at `AccessPassType`. Both destroy state. A
caller that means to remove a prepaid pass can remove an EdgeSeat pass by mistake, and the
program accepts it.

Issue #2470 asks for one instruction per pass type, and states that the pass type must not be
an instruction argument. An earlier version of this document proposed a declared type in the
args instead. Martin rejected that in the issue. This document follows the issue.

## Shape

`AccessPassType` has 5 variants, so there are 10 new instructions:

| Variant | Name | Replaces |
| --- | --- | --- |
| 119 | `ClosePrepaidAccessPass` | `CloseAccessPass` |
| 120 | `CloseSolanaValidatorAccessPass` | `CloseAccessPass` |
| 121 | `CloseSolanaRPCAccessPass` | `CloseAccessPass` |
| 122 | `CloseOthersAccessPass` | `CloseAccessPass` |
| 123 | `CloseEdgeSeatAccessPass` | `CloseAccessPass` |
| 124 | `DeletePrepaidUser` | `DeleteUser` |
| 125 | `DeleteSolanaValidatorUser` | `DeleteUser` |
| 126 | `DeleteSolanaRPCUser` | `DeleteUser` |
| 127 | `DeleteOthersUser` | `DeleteUser` |
| 128 | `DeleteEdgeSeatUser` | `DeleteUser` |

The highest variant in use today is 118.

## Components

### 1. `AccessPassKind`

A tag enum in `smartcontract/programs/doublezero-serviceability/src/state/accesspass.rs`,
next to `AccessPassType`.

```rust
#[repr(u8)]
#[derive(Debug, Copy, Clone, PartialEq, Eq)]
pub enum AccessPassKind {
    Prepaid,
    SolanaValidator,
    SolanaRPC,
    Others,
    EdgeSeat,
}

impl From<&AccessPassType> for AccessPassKind { /* one arm per variant */ }
impl fmt::Display for AccessPassKind { /* for the error message */ }
```

This type never reaches the wire. It is not part of any instruction argument, and it needs no
Borsh derive. It exists so the shared handler body can take the kind it must accept, and so the
CLI and the SDKs have one name for the choice they make.

`AccessPassType` carries payloads (a `Pubkey`, a `String` pair, a `Vec<FeedSeat>`).
`AccessPassKind` carries none, so it is `Copy` and cheap to compare.

The kind check compares the variant only. `AccessPassType::Others(type_name, key)` maps to
`Others` whatever `type_name` holds, so `CloseOthersAccessPass` closes any `Others` pass.
Pinning `type_name` would tie the instruction set to catalog data, so it is left out.

### 2. The handler bodies stay single

Ten instructions, two bodies. Close and delete take the same accounts for every kind, so the
work does not differ by kind. Only the accepted kind differs.

`processors/accesspass/close.rs`:

```rust
pub fn process_close_access_pass(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &CloseAccessPassArgs,
    expected: AccessPassKind,
) -> ProgramResult
```

`processors/user/delete.rs` gains the same trailing `expected: AccessPassKind` parameter.

Each body checks the kind right after it reads the `AccessPass`:

```rust
let actual = AccessPassKind::from(&accesspass.accesspass_type);
if actual != expected {
    msg!("instruction is for {expected} but the pass is {actual}");
    return Err(DoubleZeroError::AccessPassTypeMismatch.into());
}
```

`DoubleZeroError::AccessPassTypeMismatch` is a new variant, number 119. The highest number in
use today is 118.

The kind is a Rust parameter chosen by the dispatch arm, not an instruction argument. Ten
copies of a 250 line body would be ten places to fix the next bug in it.

### 3. Dispatch

`entrypoint.rs` gets 10 arms. Each names its kind:

```rust
DoubleZeroInstruction::ClosePrepaidAccessPass(value) => {
    process_close_access_pass(program_id, accounts, &value, AccessPassKind::Prepaid)?
}
DoubleZeroInstruction::CloseSolanaValidatorAccessPass(value) => {
    process_close_access_pass(program_id, accounts, &value, AccessPassKind::SolanaValidator)?
}
// ... 8 more
```

### 4. The arguments do not change

All 5 close instructions carry the existing `CloseAccessPassArgs`, which is empty. All 5 delete
instructions carry the existing `UserDeleteArgs`, which holds `dz_prefix_count` and
`multicast_publisher_count`. No new argument types, and no new incremental defaults to get
wrong.

No account layout changes, so the SDK fixtures do not need regeneration.

### 5. Variants 69 and 42 are replaced

They follow the pattern already used for variants 72, 75, 77 and 78. Each loses its payload and
joins the deprecated arm in `entrypoint.rs`:

```rust
CloseAccessPass(), // variant 69, deprecated: use Close<Kind>AccessPass. See #2470.
DeleteUser(),      // variant 42, deprecated: use Delete<Kind>User. See #2470.
```

Both return `DoubleZeroError::Deprecated`. The discriminants stay reserved, and a caller that
does not upgrade gets a named error rather than a silent removal.

### 6. Callers

| Caller | Change |
| --- | --- |
| `doublezero access-pass close` | new required flag `--type <prepaid\|solana-validator\|solana-rpc\|others\|edge-seat>` |
| `doublezero user delete` | new required flag `--access-pass-type`, same values |
| `CloseAccessPassCommand`, `DeleteUserCommand` (Rust SDK) | new required `kind: AccessPassKind` field; picks the instruction |
| `doublezero-serviceability-instruction` | `close_access_pass` and `delete_user` take the kind and build the matching variant |
| `doublezero disconnect` (client daemon) | fills the kind from the pass it already reads |
| `Executor.DeleteUser` (Go SDK) | new `AccessPassKind` parameter; picks the instruction number |

**Open question, asked on the issue.** The CLI takes only `--pubkey` today. For the SDK command
to pick an instruction it needs the kind from somewhere. If it reads the pass and picks from
what it read, then the on-chain refusal can never fire, because the instruction was chosen from
the same byte it checks. The guard only holds if the operator states the kind. This design
therefore adds a required flag. If Martin wants no flag, the CLI path stays unguarded and the
flag comes out.

Two callers read the kind rather than declaring it, and both are unavoidable:

- `doublezero disconnect` in the client daemon. It is a self delete, and the handler already
  checks the owner and the client IP, so the kind adds nothing there. Its `delete_users` loop
  does not hold the pass, but the `LedgerClient` trait already exposes `get_accesspass`, and the
  loop already has `client_ip` and `user.owner`, so the lookup is one line.
- `DeleteTenantCommand` with `allow_delete_users`. It sweeps every user under a tenant, and those
  users can hold passes of different kinds, so no single declared kind exists. Forcing one would
  turn "delete every user under this tenant" into "delete only users of one kind", which strands
  the tenant record, because the command then waits for `reference_count` to reach 0.

On both paths the program's refusal cannot fire: the value asserted and the value checked come
from the same account. The comment at each call site has to say so, or someone will copy the
pattern into a path where an operator could have declared the kind.

### 7. A bug to fix in the code being touched

`close.rs` wraps the account type check and the `connection_count` check in
`if let Ok(data) = accesspass_account.try_borrow_data()`. When the borrow fails, the handler
logs `Failed to borrow account data, cannot close` and then closes the pass anyway. Both checks
are skipped.

The new kind check must not sit inside that block. The fix is to read the `AccessPass` once,
before the checks, and let a failed read return an error. This is a small change in a file the
work already edits.

## Error handling

| Case | Result |
| --- | --- |
| the instruction matches the stored pass | the removal proceeds as it does today |
| the instruction is for another kind | `AccessPassTypeMismatch`, nothing is written |
| variant 69 or 42 | `Deprecated`, nothing is written |
| the account data cannot be read | an error, and the pass is not closed |

## Testing

Program tests:

- for each of the 5 kinds, one accepted close and one accepted delete;
- for each of the 5 kinds, one rejected close and one rejected delete against a pass of a
  different kind;
- variant 69 and variant 42 both return `Deprecated`.

Existing call sites move to the new instructions: `tests/accesspass_test.rs`,
`tests/user_tests.rs`, `tests/delete_user_dynamic_accesspass.rs`, `tests/user_old_test.rs`,
`tests/accesspass_allow_multiple_ip.rs`, `tests/create_subscribe_user_test.rs`,
`tests/multicastgroup_subscribe_test.rs`, `tests/user_onchain_allocation_test.rs`.

CLI tests: the new flag is required, and an unknown value is rejected.

Go SDK: `user_crud_test.go` passes the new parameter.

## Rollout

This breaks callers that do not upgrade. They get `Deprecated`.

The program, the instruction crate, the Rust SDK, the CLI and the Go SDK land together in this
repository.

The oracle lives in `doublezero-shreds`. Parent issue #2385 lists five places there that remove
users: `cleanup_orphaned_users`, the lapsed seat branch of `reconcile_validator_owned_users`,
`converge_retransmit_only_seats`, `process_instant_withdrawal_requests`, and
`access_pass_expiry`. Each one knows which class of user it cranks, so each one has a real kind
to name. That is where the risk in #2385 sits. It needs its own change, shipped with this
program deploy.

## PR size

The 10 enum variants touch 5 places each in `instructions.rs` (the enum, the decoder, the name,
the debug format, and the round trip test), plus 10 arms in `entrypoint.rs`. That is mechanical
but wide. If the total goes past the 500 line guideline in `CLAUDE.md`, the work splits into two
PRs: the program and the instruction crate first, then the CLI and the SDKs.

## Out of scope

Feed subscription has no access pass or user instructions. Issue #2470 records the rule for
future instructions there. No code changes.
