# RFC-26: Rust Instruction-Builder Library for the Serviceability Program

## Summary

**Status: `Draft`**

This RFC proposes a new Rust crate, `doublezero_serviceability_instruction`, that provides one pure builder function per `doublezero-serviceability` instruction. Each builder takes already-resolved arguments and returns a single unsigned `solana_program::instruction::Instruction`, with no RPC access — the classic SPL pattern (`spl-token::instruction::transfer(...)`). The caller composes the `Transaction`, prepends the compute-budget prelude, adds the blockhash, and signs.

Today the assembly logic for a serviceability instruction (PDA derivation, `AccountMeta` ordering, borsh packing) is coupled to execution: every `smartcontract/sdk/rs/src/commands/<domain>/<verb>.rs` has an `execute()` that builds *and* signs+sends the transaction, so there is no reusable way to construct an instruction offline. The goal is to make instruction construction reusable, offline, dependency-light, and the single source of truth for account layout. The work is scoped to **Rust only**; Go/Python/TS parity is a later phase.

## Motivation

The serviceability program exposes 116 instruction variants. The only supported way to build any of them is through the SDK command layer, where construction is entangled with signing and sending:

- **No pure builders.** A caller that wants an unsigned `Instruction` — to batch it, simulate it, inspect it, or sign it with a different signer — cannot get one. It must go through `XxxCommand::execute()`, which owns the RPC client and the send path.
- **Heavy dependency to build one instruction.** Any consumer that needs instruction bytes today must depend on `doublezero_sdk`, which pulls `solana-client`, `tokio`, `backon`, and the rest of the RPC tree. Bots, indexers, and the fixture generator pay that cost just to lay out accounts and borsh-pack args.
- **Account ordering is duplicated and drift-prone.** The processor (`next_account_info` order) and each command (`AccountMeta` vec) independently encode the same layout; when they disagree the processor rejects the transaction at runtime. The trailing `[payer, system_program, permission?]` convention lives in `client.rs::assemble_instructions`, so every command must line its accounts up with that tail.
- **Dangerous length-detection is implicit.** `CreateUser` detects an optional trailing tenant account via `accounts.len()` and never calls `authorize()`. If a caller also appends a Permission PDA, it corrupts that parsing. Nothing in the current API prevents this. (`DeleteUser` detects its optional tenant from onchain state and `CreateSubscribeUser` via `split_trailing_permission`, so both tolerate a trailing Permission account — only `CreateUser` is length-fragile.)

The fix is the split SPL uses: a pure, RPC-free instruction library beneath the RPC-bearing SDK, with the account-order convention centralized in one reviewable place and backed by golden fixtures and `solana-program-test` tests.

## New Terminology

- **Builder** — a pure function `build_xxx(...) -> Instruction` that assembles exactly one serviceability instruction from resolved arguments. Infallible; no RPC.
- **Pure / offline** — the builder performs no network I/O. Chain-derived values (globalstate `account_index`, `dz_prefix` count) are passed in as explicit parameters by the caller.
- **Trailing convention** — the fixed tail of every instruction's account list: `[payer (signer, writable), system_program (readonly)]`, followed — for instructions whose `authorize()` migration is activated — by the read-only Permission PDA (derived from the payer) as the last account.
- **`authorize()`-gated instruction** — an instruction whose processor calls `authorize()`; once migrated, its builder appends the trailing Permission account (see [Permission account](#permission-account)).
- **Length-detected family** — `CreateUser` (36): its processor identifies an optional trailing tenant account by `accounts.len()` and never calls `authorize()`. Its builder never appends a Permission account, permanently, so the length count stays unambiguous. (`DeleteUser` and `CreateSubscribeUser` were originally grouped here but are not length-detected — see below.)
- **`split_trailing_permission` family** — instructions with a variable-length account list followed by payer/system and then, once migrated, the payer-derived Permission PDA, which the processor peels off by PDA match (link delete, user update, `CreateSubscribeUser`, interface update, topology assign, multicast allowlists). `DeleteUser` similarly calls `authorize()` positionally after detecting its optional tenant from onchain state, so it too can carry a trailing Permission account.
- **Golden fixture** — a committed `ix_<name>.bin` (wire bytes) and `ix_<name>.json` (variant, `data_hex`, ordered accounts with flags) capturing a builder's deterministic output, guarded in CI.

## Alternatives Considered

- **Do nothing.** Keep construction inside the command layer. Rejected: leaves account ordering duplicated, keeps the heavy dependency for external consumers, and never removes the length-detection hazard from the API surface.
- **A `builders/` module inside `doublezero_sdk`.** Rejected: loses the dependency isolation — the module could still reach RPC, and external consumers would still transitively depend on `solana-client`/`tokio`. Purity would be a convention, not an invariant.
- **Builders inside the on-chain program crate.** Rejected: bloats the BPF binary with host-only code and adds unnecessary on-chain review burden for what is a host-side concern.
- **A proc-macro generating builders from the processor definitions.** Rejected: it would hide the account order reviewers most need to see. The anti-drift mechanism is instead a centralized `common::build`, verbatim account-layout doc-comments copied from each processor, and `solana-program-test` tests against the real program.
- **Return a `Vec<Instruction>` or a `Transaction`.** Rejected: a builder returns a single pure `Instruction`, SPL-style. The compute-budget prelude is transaction-level and exposed separately; composition, blockhash, and signing belong to the caller.

## Detailed Design

### Crate location and dependency graph

Create `crates/doublezero-serviceability-instruction/`, crate `doublezero_serviceability_instruction`, registered as a workspace member. The acyclic dependency graph enforces purity — the crate cannot reach RPC because it never depends on the RPC tree:

```
doublezero-serviceability            (enum, *Args, pda, resource types)
      ^
doublezero_serviceability_instruction   <- PURE builders
      |  deps: solana-program, doublezero-program-common ONLY
      |  (NO solana-client, tokio, backon)
      ^
doublezero_sdk (sdk/rs)              -> commands/* delegate to the builders
```

This mirrors the `spl-token` split (pure `instruction` module beneath the RPC-bearing client).

### Layout

One module per domain, mirroring `processors/` and `commands/`:

```
crates/doublezero-serviceability-instruction/
  Cargo.toml
  src/lib.rs        # re-exports domains; compute_budget_prelude(); consts;
                    #   documented list of excluded deprecated variants
  src/common.rs     # build() — the single place that knows the trailing convention
  src/device.rs  link.rs  user.rs  location.rs  exchange.rs  contributor.rs
  src/multicastgroup.rs  tenant.rs  permission.rs  topology.rs  feed.rs
  src/accesspass.rs  resource.rs  globalstate.rs  globalconfig.rs  allowlist.rs
  src/index.rs  migrate.rs
```

### The anti-drift keystone: `common.rs`

`common::build` is the single place that appends the trailing accounts. Wire encoding is `1 tag byte + borsh(args)`, obtained for free by constructing the `DoubleZeroInstruction` variant and calling `.pack()` (which is `borsh::to_vec`) — no hand-written tag bytes.

```rust
pub(crate) fn build(
    program_id: &Pubkey,
    instruction: DoubleZeroInstruction,
    mut accounts: Vec<AccountMeta>,   // instruction-specific metas in processor order,
                                      //   WITHOUT payer/system
    payer: &Pubkey,
) -> Instruction {
    accounts.push(AccountMeta::new(*payer, true));
    accounts.push(AccountMeta::new(solana_system_interface::program::id(), false));

    // Permission PDA (authorize()) — derived from the payer, not passed in.
    // Left commented until the instruction's authorize() migration is activated
    // (see "Permission account" below):
    //
    // let (permission, _) = get_permission_pda(program_id, payer);
    // accounts.push(AccountMeta::new_readonly(permission, false)); // must be last

    Instruction::new_with_bytes(*program_id, &instruction.pack(), accounts)
}

// Transaction-level prelude, NOT inside each builder.
// CU limit 1_400_000, heap 256 KiB — mirrors client.rs::MAX_COMPUTE_UNIT_LIMIT /
// MAX_HEAP_FRAME_BYTES.
pub fn compute_budget_prelude() -> [Instruction; 2] { /* ... */ }
```

This reproduces the payer/system tail that `client.rs::assemble_instructions` builds today, extracted into one location. It omits the Permission PDA that `assemble_instructions` appends today; that append is **deferred** (see below).

### Permission account

The Permission PDA is **deterministically derived from the payer** (`get_permission_pda(program_id, payer)`), so it is **never a caller-supplied argument** — no builder takes a `permission: Option<Pubkey>`, and no caller can substitute an arbitrary account. Whether an instruction expects the trailing Permission account is not something the offline builder can observe at runtime; it is the per-instruction fact of whether that instruction's `authorize()` migration is activated.

The rollout therefore tracks the program's incremental `authorize()` migration. Initially builders emit no Permission account — exactly what each pre-migration processor expects. As each instruction is migrated, its builder derives and appends the payer's PDA read-only as the last account; those two lines ship **commented out** in `common.rs` (above), enabled per-builder by the activating PR.

### Canonical builder signature

The rule for each parameter:

- **Offline-derivable PDAs are derived inside the builder** using `pda.rs` helpers (`get_device_pda`, `get_globalconfig_pda`, `get_resource_extension_pda`, etc.); the Permission PDA likewise (see [Permission account](#permission-account)), so none is a parameter.
- **Non-derivable external accounts are passed as `&Pubkey`**: contributor, location, exchange, device, mgroup, accesspass, side_a/side_z, feed, tenant.
- **RPC-derived scalars are passed explicitly**: `account_index: u128`, `dz_prefix_count: u8`.
- **Returns an infallible `Instruction`.** Infallible normalization that affects the wire (e.g. `code.make_ascii_lowercase()`) may happen in the builder; fallible validation (charset/length) stays in the caller.

```rust
// device.rs — variable dz_prefix blocks (builder fixes args.resource_count)
pub fn create_device(
    program_id: &Pubkey, payer: &Pubkey,
    contributor: &Pubkey, location: &Pubkey, exchange: &Pubkey,
    account_index: u128, mut args: DeviceCreateArgs,
) -> Instruction;

// link.rs — fixed accounts
pub fn create_link(
    program_id: &Pubkey, payer: &Pubkey,
    contributor: &Pubkey, side_a: &Pubkey, side_z: &Pubkey,
    link_index: u128, args: LinkCreateArgs,
) -> Instruction;

// user.rs — split_trailing_permission; optional feed appended before payer/system
pub fn create_subscribe_user(
    program_id: &Pubkey, payer: &Pubkey,
    device: &Pubkey, mgroup: &Pubkey, accesspass: &Pubkey,
    dz_prefix_count: u8, feed: Option<Pubkey>,
    args: UserCreateSubscribeArgs,
) -> Instruction;
```

### Variable-account instructions

- **`dz_prefix` blocks** (`create_device`, `create_subscribe_user`): the builder loops `0..count` deriving `ResourceType::DzPrefixBlock(entity, idx)` PDAs, then writes the derived count back into the Args `resource_count` field. Count and account list are produced from the same loop, so they can never disagree.
- **Length-detected optional trailing** (`CreateUser` only — tenant): the optional account is appended conditionally **before** payer/system. Because `CreateUser` never calls `authorize()`, its builder never appends a Permission account — permanently — so the hazard of a Permission PDA corrupting `accounts.len()` detection cannot arise; a test pins the account count. (`DeleteUser` detects its tenant from onchain state and `CreateSubscribeUser` uses `split_trailing_permission`, so neither is length-fragile.)
- **`split_trailing_permission` family** (link delete, user update, `CreateSubscribeUser`, interface update, topology assign, multicast allowlists; and `DeleteUser`): variable list, then payer/system, then — once migrated — the payer-derived Permission PDA last (the same deferred, activate-in-one-place append; safe because the processor peels it by PDA match, so the optional feed/tenant that sits before payer/system is never confused with it).
- **Batched instructions** (`clear_topology`, `assign_topology_node_segments`): a single-chunk builder plus a `*_batched(...) -> Vec<Instruction>` convenience. The batch-size consts move into this crate; the 32-account cap math accounts for the trailing accounts the builder now owns.

### Coverage and excluded variants

The enum has 116 variants (tags 0–115, contiguous). Builders cover all buildable variants (~94). Excluded, documented in `lib.rs`, are:

- **Explicit placeholders** kept only for discriminant stability: `Deprecated95`, `Deprecated96`, `Deprecated102`, `Deprecated103`, `Deprecated111`.
- **Deprecated handlers** that return `DoubleZeroError::Deprecated`: the device lifecycle variants `ActivateDevice`, `RejectDevice`, `SuspendDevice`, `ResumeDevice`, `CloseAccountDevice`, and the corresponding link/user/multicast lifecycle variants (`Suspend*`/`Resume*` for device/link/user are all deprecated — suspend/resume now go through `UpdateDevice`/`UpdateLink`/`UpdateUser` with `desired_status`), plus several `*DeviceInterface` variants. Because `suspend_device` is deprecated, the R0 exemplar set uses `delete_device` in its place (a variable-account builder with RPC-read owner accounts and a legacy/atomic split).

No `Err`/panic stubs are emitted — builders are infallible; excluded variants simply have no builder, with the exclusion list documented.

### Migrating `commands/*` (final, riskiest phase)

- Add to the `DoubleZeroClient` trait: `send_transaction(Instruction) -> Result<Signature>` (plus a quiet variant). The permission cache (`note_transaction_sent`/invalidation) stays; since migrated builders derive the Permission PDA internally, the command layer no longer resolves or passes a permission pubkey (see Open Questions).
- `client.rs::assemble_instructions` stops appending payer/system (that now lives in `common::build`); it only prepends the compute-budget prelude over a pre-built `Instruction`. This leaves exactly one owner of the trailing accounts, removing the risk of the two layers disagreeing.
- Each `execute()` becomes: validate → resolve RPC values → call the builder → `client.send_transaction(ix)`. The `mockall` command tests change shape to assert on the built `Instruction` (now including payer/system).

### Testing and golden fixtures

Extend the Rust generator `sdk/serviceability/testdata/fixtures/generate-fixtures/src/main.rs` (which already emits `user_create_args`/`user_delete_args`) to depend on the builder crate and emit, per instruction, with deterministic inputs and a fixed `program_id`:

- `ix_<name>.bin` = `instruction.data` (tag + borsh) → the wire bytes.
- `ix_<name>.json` = `{ variant, data_hex, accounts: [{pubkey, is_signer, is_writable}] }` → captures account order + flags.

Consumers:

- **Rust unit tests** in the crate port the existing `mockall` expectations (which assert the exact `Vec<AccountMeta>`) onto `Instruction.accounts`, plus `data[0] == <tag>`.
- **`solana-program-test` integration tests** in `programs/doublezero-serviceability/tests/`: for each buildable variant, build via the builder and run it against the real in-process program. If the account order is wrong, the processor fails. This is the refactor's safety net.
- Fixtures remain available to verify Go/Python/TS parity in a later phase (out of scope now). CI fails on uncommitted fixture diffs (`make generate-fixtures`).

Priority: variable-account builders + the create-family (20, 28, 59) + topology (109, 110), then the rest.

### Rollout

Respecting the ~500-lines-of-new-code-per-PR norm (tests excluded):

| PR | Scope |
|----|-------|
| R0 | Scaffold crate + `common` + `compute_budget_prelude` + 4 exemplar builders (`create_device`, `create_link`, `delete_device`, `create_subscribe_user`) + first fixtures |
| R1 | `device` domain builders |
| R2 | `link` domain builders |
| R3 | `user` domain builders (length-detected `CreateUser` + split_trailing_permission `CreateSubscribeUser`) |
| R4 | `location` + `exchange` + `contributor` builders |
| R5 | `multicastgroup` builders (+ pub/sub allowlists) |
| R6 | `tenant` + `permission` builders |
| R7 | `topology` (incl. batched) + `feed` builders |
| R8 | `accesspass` + `resource` builders (data-bearing enums) |
| R9 | `globalstate` + `globalconfig` + `allowlist` + `index` + `migrate` builders |
| R10 | Migrate `commands/*` to delegate + trait methods + `solana-program-test` suite |
| RF | Extend fixture generator (`ix_*.bin/.json`) + CI diff guard (can land alongside R0) |

## Impact

- **New codebase surface.** A new workspace crate (~18 modules, one per domain) and its tests. No new runtime dependency for the on-chain program; the crate depends only on `solana-program` and `doublezero-program-common`.
- **SDK refactor (R10).** `commands/*` `execute()` methods are rewritten to delegate to builders; `assemble_instructions` shrinks to only prepending the prelude; two methods are added to the `DoubleZeroClient` trait (currently 19 methods). Command-layer `mockall` tests are re-pointed onto the built `Instruction`.
- **Lighter external consumers.** Bots, indexers, and the fixture generator can build instructions without the RPC tree — a meaningful compile-time and dependency-surface reduction.
- **Operational complexity.** No deployment change and no wire change to the instruction-specific accounts (tag, borsh args, caller-account order unchanged); the only moving part is the deferred Permission-account append (see Backward Compatibility). Otherwise a code-organization and API-surface change.
- **Performance.** No on-chain or runtime impact. Instruction bytes (tag + borsh) and the instruction-specific account layout are byte-for-byte identical, verified by golden fixtures.
- **Documentation.** Each builder carries a verbatim account-layout doc-comment copied from its processor, giving reviewers a single readable statement of the account order.

## Security Considerations

- **Account-order drift (biggest risk).** A builder whose account order diverges from its processor produces transactions the processor rejects — and, worse, a subtly wrong order could target the wrong account. Mitigations: `common::build` centralizes the trailing convention; verbatim doc-comments; and the `solana-program-test` suite runs every buildable variant against the real program so a wrong order fails a test.
- **Trailing-convention regression.** With the layout owned in two layers (each command's account vec and `assemble_instructions`), the two could drift. After migration there is exactly one place (`common::build`) that appends payer/system; `assemble_instructions` no longer does.
- **Permission account.** The Permission PDA is derived from the payer inside the builder, never passed in, so a caller cannot substitute an arbitrary account to spoof the trailing slot. The append is centralized in `common::build_with_permission` (deferred; enabled in one place at rollout); builders that never route through `authorize()` call `common::build` and can never carry one. For `CreateUser` (the length-detected family) it is never appended — appending one would corrupt the `accounts.len()` detection — and a test pins the account count.
- **Count/account mismatch.** For `dz_prefix` blocks, the builder derives the count from the same loop that produces the accounts and writes it back into the Args, so the declared count can never disagree with the account list.
- **Discriminant coupling.** Builders never hand-write tag bytes; they construct the `DoubleZeroInstruction` variant and call `.pack()`, getting the correct tag and borsh encoding for free.
- **No new trust boundary.** The crate is host-side and RPC-free; it introduces no new signing authority and no on-chain code.

## Backward Compatibility

The serviceability program is unchanged, and for each instruction the tag byte, borsh args encoding, and the order of the instruction-specific accounts are identical before and after. The golden fixtures and the `solana-program-test` suite prove that part carries no behavioral change.

The one deliberate exception is the trailing Permission account. Today `assemble_instructions` appends the payer's Permission PDA whenever it resolves; the migrated builders defer that append until each instruction's `authorize()` migration is activated. So until an instruction's append is re-enabled, its migrated builder emits a shorter account list, and `authorize()` takes its no-permission path (the legacy GlobalState allowlist, unless `RequirePermissionAccounts` is enforced for it). This must be sequenced against the permission-model rollout so no caller loses authorization mid-transition (see Open Questions).

The remaining API change is additive/internal to the Rust SDK: the new `DoubleZeroClient` send method(s) and the `assemble_instructions` refactor. Consumers of the `commands/*` layer see no behavioral difference beyond that transition.

## Open Questions

- **Go/Python/TS parity.** Out of scope here. The golden fixtures are designed to drive parity checks for the other SDKs later; the concrete cross-language builder API is deferred.
- **`clear_topology` / `assign_topology_node_segments` batch-size constants.** The exact home for the moved consts and whether the 32-account cap math needs adjustment once the builder owns the trailing accounts should be confirmed during R7.
- **Naming of the trait send method.** `send_transaction` (plus a quiet variant) is proposed; the final name/shape is to be settled in R10 review.
- **Permission append: static activation vs. runtime existence.** This RFC replaces today's runtime existence check (the memoized cache / foundation-lockout fallback) with a static, per-instruction "is `authorize()` activated" decision (see Backward Compatibility). Whether any instruction still needs the runtime check once its builder append is on must be settled as the model rolls out.
- **Fixture naming collisions.** Whether any two builders map to the same `ix_<name>` base (e.g. create vs. create-subscribe) needs a disambiguation convention when the generator is extended in RF.
