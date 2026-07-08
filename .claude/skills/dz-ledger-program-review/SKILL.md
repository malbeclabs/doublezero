---
name: dz-ledger-program-review
description: Use when reviewing on-chain program (Rust) code or PRs in this repo — the DoubleZero Ledger serviceability and geolocation programs (borsh serde + AccountType enum). Complements smartcontract/programs/CLAUDE.md (the canonical rules) with diff-scan checklists, real review examples, severity guidance, and a review process. Covers sibling-processor parity, account/signer validation, PDA seeds+bump, access control, instruction validation, checked arithmetic, rent/space/heap caps, error handling, off-chain RPC hygiene, and testing.
---

# DoubleZero Ledger program review

This skill distills the review conventions actually applied to on-chain program PRs in `malbeclabs/doublezero` — specifically the DoubleZero **Ledger** programs under `smartcontract/programs/` (the serviceability and geolocation programs). Apply it when reviewing Rust that touches these programs or their instruction processors, state accounts, and client/off-chain helpers.

**Source of truth & precedence.** `smartcontract/programs/CLAUDE.md` is the canonical statement of these programs' conventions and is always loaded in this repo. This skill does not restate those rules — it adds what CLAUDE.md doesn't: concrete "what to look for in a diff" checklists, real review examples with PR links, severity guidance, and a review process. Where a rule is codified in CLAUDE.md, this skill cites it (e.g. "CLAUDE.md Security §1") instead of repeating it. **On any conflict, CLAUDE.md wins** — treat it as correct and flag the drift. A few checks below are curated from reviewer guidance not yet quote-backed by a mined PR; they are marked *(reviewer-curated)*.

**Serialization model — read this first.** These programs use **borsh serde for account (de)serialization** with a **custom 1-byte `AccountType` enum discriminator**. Accounts are read via `T::try_from(&AccountInfo)`, which borsh-deserializes the data and checks the **discriminant only** (`account_type == AccountType::X`). It does **not** check `account.owner` — every processor must verify ownership separately (CLAUDE.md Security §1), with the singleton exception (ProgramConfig, GlobalState) where the discriminant check alone suffices. Do not assume `try_from` pins identity. State creation/mutation/close goes through the `try_acc_create` / `try_acc_write` / `try_acc_close` helpers. Authority is enforced with `GlobalState` + `authorize(...)` + `permission_flags`. PDAs are derived with `find_program_address`, with the bump cached on the account only when it must sign. Errors use the `DoubleZeroError` custom enum, and validation is done with `assert!` / `assert_eq!` and `return Err(DoubleZeroError::…)`.

**This is NOT a bytemuck / zero-copy program.** Solana zero-copy conventions (fixed Pod layouts, 8-byte precomputed discriminators, `try_next_accounts` account loaders, etc.) do **not** apply here and should never be suggested — the discriminator is a plain 1-byte `AccountType` enum and accounts are borsh-serialized. If a suggestion only makes sense for a zero-copy program, it is wrong for this repo.

Review window for these conventions: **2025-07 through 2026-03**.

## How to apply — severity and voice

Report findings at their real weight; a review that flags everything equally is noise.

- **Blocking** (fix before merge): real correctness bugs; a missing owner check (CLAUDE.md Security §1); arithmetic over/underflow on counts, seats, or lamports; a collection cap not enforced at the mutating instruction (heap-brick risk); a missing changelog entry (CI-enforced).
- **Nit** (raise, non-blocking): naming/idiom, debug/`Display` representations, other style items — label them as nits.
- **Follow-up** (defer to a separate PR): broad cosmetic sweeps unrelated to the change.

Match the voice the examples model:
- Frame judgment calls as questions ("when would this ever be true?", "should this be `checked_sub`?"), not commands.
- Give the exact replacement code, not just a complaint.
- Prove a serialization/layout claim with a runnable `#[test]` (e.g. a borsh round-trip), don't just assert it.
- Distinguish severity in words — "this is more than just a suggestion" for blocking, "up to you" for optional.

Then apply the classes below one at a time against the diff, highest-signal first.

---

### Sibling-processor parity

**Current guidance:** Serviceability is a family of parallel processors (create/update/suspend/resume/delete × device/link/user/exchange/…). A new instruction or entity should match its closest established sibling unless there's a stated reason to differ. Before reviewing a new processor in isolation, diff it against the nearest sibling and reconcile every divergence: the same `authorize()` / `permission_flags` gating, the same status-transition handling (suspend/resume/activate), the same `reference_count` increment/decrement discipline, and the same account-validation shape. Drift-from-sibling is its own bug class — the per-class checks below each look at the diff in isolation and will miss it. *(Reviewer-curated: a recurrent review theme rather than a single quoted PR.)*

**Why it matters:** These processors are near-copies by design; silent divergence in authorization, status handling, or counters is almost always an oversight, and it reads as "fine" when you only look at the new file.

**What to look for in a diff:**
- A new processor gated by different `authorize()`/`permission_flags` than its siblings, with no explanation.
- A new entity with a `reference_count` (or similar counter) that increments but has no matching decrement path, or that diverges from how siblings manage it.
- A create/update/delete that skips a validation or status transition its sibling performs.
- A new status field whose transitions don't mirror the established suspend/resume/activate flow.

---

### Testing & coverage

**Current guidance (as of 2026-02-27):** CLAUDE.md Testing §1–§4 already require asserting specific error types (never a bare `.is_err()`), full-struct `PartialEq` equality, not testing framework/SDK behavior, and integration tests for every processor (success, all error paths, edge cases, state transitions) — the diff-scan for those is below. Beyond CLAUDE.md: add an SVM test that proves `validate()` catches invalid input; after an instruction, deserialize the account and assert header/field contents (not just that the call succeeded); check a computed expected value (e.g. total block rewards), not a smoke test; and cover schema-migration/upgrade paths (an old-schema account upgrading) and boundary sizes (a collection grown near the 32KB heap max — see Rent / heap caps). **Reach the target state through real instruction sequences, not `set_*`/poke helpers that write account bytes directly.** Flag tests gated behind a feature the default CI build never enables — they give false coverage.

**Why it matters:** A test that only checks `.is_err()` passes even when the instruction reverts for an unrelated reason, hiding real regressions. Field-by-field assertions silently skip new fields. Untested migration paths ship broken upgrades that can brick live accounts. Byte-poked state tests a shape the real instruction path can't actually produce; feature-gated tests that never run are coverage theater.

**What to look for in a diff:**
- New tests using `.is_err()` / `.is_ok()` instead of matching `ProgramError::Custom(n)`.
- Assertions that compare individual fields where the struct derives `PartialEq` and could be compared whole.
- Tests that only assert an SDK/framework property (determinism of a PDA derivation, etc.).
- A new instruction with a `validate()`/invariant but no SVM test exercising the invalid-input path.
- Reward/aggregation math verified only by "it ran" rather than an expected total.
- Schema/version changes with no test upgrading an old-schema account.
- State reached via `set_*`/direct byte pokes instead of real instruction calls.
- A test behind a `#[cfg(feature = ...)]` the default CI build doesn't enable.

**Examples:**
- "Assert specific errors: Tests should assert specific error types (e.g., `ProgramError::Custom(17)`), not just `.is_err()`. This catches regressions where the instruction fails for the wrong reason. ... Don't test framework functionality: Avoid writing tests that only exercise SDK/framework behavior (e.g., testing that `Pubkey::find_program_address` is deterministic...)" — codified in `smartcontract/programs/CLAUDE.md`. [malbeclabs/doublezero#3120](https://github.com/malbeclabs/doublezero/pull/3120)
- "if GeoProbe derives `PartialEq` then it would be better to `assert_eq!(probe, expected_probe)` in case you add more fields to your geo probe account in the future. With the way this is written, you run the risk of missing asserting that the new fields will be equal what you expect" — on `test_create_geo_probe_success()`. [malbeclabs/doublezero#3120](https://github.com/malbeclabs/doublezero/pull/3120)
- "Can you add an SVM program test to confirm that validate catches invalid input when this instruction is called?" [malbeclabs/doublezero#2288](https://github.com/malbeclabs/doublezero/pull/2288)
- "Bump. We really should be checking for an expected total calculated block rewards." — on a test that always fetches validator rewards from epoch 812. [malbeclabs/doublezero#729](https://github.com/malbeclabs/doublezero/pull/729)

---

### Account & signer validation

**Current guidance (as of 2026-02-24):** Ownership/identity rules are codified in CLAUDE.md Security §1–§3 (verify the owner of program-owned accounts; singleton discriminant exception; the System-program check is unnecessary; PDA validation self-confirms derivation) and Code Org §3 (don't pass a key as an arg and then assert it matches a passed account) — the diff-scan for those is below. **One addition:** the *expected* owner/program id must come from a hardcoded in-repo constant (e.g. `crate::serviceability_program_id()`), never from an instruction arg or a caller-passed account — a caller-supplied "expected owner" is no check at all.

**Why it matters:** A missing owner check lets an attacker substitute a look-alike account owned by a different program and bypass all downstream trust. Redundant checks the runtime already enforces add compute cost and noise without adding safety. And an owner check against a caller-supplied value is worse than none — it looks like a guard but enforces nothing.

**What to look for in a diff:**
- A program-owned account read without an owner check against the serviceability program id or `program_id`.
- An owner/program-id check whose expected value comes from an instruction arg or a passed account instead of a hardcoded const (`serviceability_program_id()`) — not a real check.
- An explicit check on the System program account's key.
- A manual PDA re-derivation/assert on a singleton account that `try_from` already pins via its discriminator.
- A "payer is writable signer" assert guarding a System transfer.
- An instruction arg carrying a key that also appears on a passed-in account, followed by an equality assert — take it from the account instead.

**Examples:**
- "I would say this is more than just a suggestion. You should verify program IDs (for serviceability accounts, make sure the owner is serviceability program ID, and for your own accounts, make sure the owner is `program_id`)". [malbeclabs/doublezero#2961](https://github.com/malbeclabs/doublezero/pull/2961)
- "Checks for the system program shouldn't be necessary because the system interface builds instructions by using the system program as the program ID (so if the system program were not provided, you would get a revert)" — on `let _system_program = next_account_info(accounts_iter)?;`. [malbeclabs/doublezero#2961](https://github.com/malbeclabs/doublezero/pull/2961)
- "This actually isn't needed when updating the program config since there is only one account that can be the program config... The discriminator defines this account, so the `try_from` below ensures that this account is the program config. It doesn't hurt to have this check. But it doesn't add any safety, either". [malbeclabs/doublezero#3083](https://github.com/malbeclabs/doublezero/pull/3083)
- "This check feels unnecessary. Because you're passing in the device account you care about, you should just be able to take that key. Having the instruction argument specifying this key and checking what you passed in the account is superfluous" — on `process_add_parent_device`. [malbeclabs/doublezero#2961](https://github.com/malbeclabs/doublezero/pull/2961)

---

### Naming & idiom / code style

**Current guidance (as of 2025-12-18):** Enforce project naming conventions and idiomatic Rust. Use the adopted domain vocabulary — call an authority a "Sentinel" / `SetSentinel` to match the Passport and Revenue Distribution programs rather than a generic name, and label a newly created PDA after its instruction (`access_pass_account`) so it is auditable. Suffix `AccountInfo` bindings with `_info` (`device_info`, not `device`), and name a constant for its general purpose, not the one incidental call site that uses it. Fix casing (no camelCase for pubkeys). Prefer idiomatic constructions: `Contributor::try_from(acct)?` rather than restating the type on the binding; derive `Default` with `#[default]` on an enum instead of a hand-written match; use `Self::` inside impl blocks. Remove `msg!`/print statements added solely to silence an unused-variable warning, and drop unneeded function parameters (a processor with no args should take just `program_id` and `accounts`).

**Why it matters:** Consistent vocabulary and PDA labels make audits and log reading tractable across the program family. Idiomatic constructions and removing noise keep the surface reviewable.

**What to look for in a diff:**
- A generic authority name where the family already uses "Sentinel".
- A newly created PDA bound to a name that does not describe its instruction/role.
- An `AccountInfo` binding without the `_info` suffix.
- Redundant type annotations on `try_from` bindings; hand-written `Default` matches; missing `Self::`.
- `msg!`/`print`/`_ = x` patterns whose only purpose is silencing unused-variable warnings.
- Processor signatures carrying args they never use.

**Examples:**
- "This may be too generic sounding. Can we use the same naming convention we've adopted for the Passport and Revenue Distribution programs and call this the Sentinel? `SetSentinel`". [malbeclabs/doublezero#1272](https://github.com/malbeclabs/doublezero/pull/1272)
- "Can we start labeling the newly created PDAs something relevant to the instruction? It would be easier to audit if you called this `access_pass_account`." — on `process_set_access_pass`. [malbeclabs/doublezero#1272](https://github.com/malbeclabs/doublezero/pull/1272)
- "the type doesn't have to be defined here. Should be able to either: `let mut contributor = Contributor::try_from(contributor_account)?;`" — on `let mut contributor: Contributor = Contributor::try_from(contributor_account)?;`. [malbeclabs/doublezero#2489](https://github.com/malbeclabs/doublezero/pull/2489)
- "This feels like an anti-pattern of printing/logging a variable meant to avoid the unused variable warning. You can probably remove this and add logging later if there are ever any migration args" — on `process_migrate`. [malbeclabs/doublezero#2332](https://github.com/malbeclabs/doublezero/pull/2332)

---

### Instruction / data validation

**Current guidance (as of 2026-03):** Reject clearly invalid or nonsensical input at the instruction boundary — but reject it, do not silently no-op or filter. Revert (do not filter out) when a collector submits zero samples or any zero values, because a zero means the collector is misbehaving and the revert surfaces in logs for debugging. Add a revert when `actual_key == new_key` on a migrate so passing the same account twice cannot become a silent no-op. Equally, do not add redundant or impossible validation: don't re-check a condition already guaranteed elsewhere (a code length already validated at create time, or a case `find_program_address` would already reject), and don't validate a value you can just take directly from the passed account (CLAUDE.md Code Org §3). Remove validation that duplicates a check done in another processor.

**Why it matters:** Silently filtering bad input hides a broken producer and corrupts downstream data; a silent no-op on a mistaken migrate looks like success. Redundant validation costs compute and misleads readers into thinking a real invariant is being enforced there.

**What to look for in a diff:**
- Zero-length / zero-value inputs being filtered or no-op'd instead of reverted.
- A migrate/move instruction that does not reject `from == to` (same account passed for both).
- A re-validation of length/bounds already enforced at create time or by a PDA seed.
- A `data.is_empty()` (or similar) check that is redundant with a check right beside it.

**Examples:**
- "I think reverting if any zeros are detected is nice. Then we can debug the collector to see why it is trying to submit zeros when we track these reverts in its logs." — on `WriteInternetLatencySamplesArgs`. [malbeclabs/doublezero#888](https://github.com/malbeclabs/doublezero/pull/888)
- "To prevent potential spam from a rogue internet collector process, can we enforce that the len of samples is != 0?" [malbeclabs/doublezero#888](https://github.com/malbeclabs/doublezero/pull/888)
- "we should add a revert if actual key == new key for the migrate instruction to avoid a no-op if someone were to pass in the same account for each of these accounts" — on `process_migrate`. [malbeclabs/doublezero#2332](https://github.com/malbeclabs/doublezero/pull/2332)
- "can you remove the `data.is_empty()` check in `CloseAccessPass`? It is redundant with this check" — on `process_close_access_pass`. [malbeclabs/doublezero#2289](https://github.com/malbeclabs/doublezero/pull/2289)

---

### Account (de)serialization

**Current guidance (as of 2026-02-24):** CLAUDE.md Serialization codifies preferring borsh derives (`BorshDeserialize`, or `BorshDeserializeIncremental` when fields may be added over time) over hand-rolled deserialization; the diff-scan and examples here support that. State structs and instruction-arg structs should derive rather than manually deserializing field-by-field or writing custom deserialize methods that merely wrap the borsh methods. Instruction `pack`/`unpack` helpers that are thin wrappers over `borsh::to_vec` / `from_slice` are unnecessary. For forward-compatibility, plain `T::deserialize(&mut &buf[..])` already ignores trailing bytes, so a custom `compat_deserialize` helper is redundant. Borsh enums pack efficiently — the variant tag plus only that variant's fields.

**Why it matters:** Hand-written (de)serialization drifts from the derived layout and introduces subtle field-ordering or length bugs; wrapper helpers add surface with no benefit and obscure what borsh already guarantees, including forward-compatible trailing-byte handling.

**What to look for in a diff:**
- A manual `deserialize` impl that reads each field in sequence where `#[derive(BorshDeserialize)]` would do the same.
- `pack`/`unpack` methods that just call `borsh::to_vec(self).unwrap()` / `from_slice`.
- A `compat_deserialize`-style helper duplicating `T::deserialize(&mut &buf[..])`.
- A struct deriving `BorshSerialize` but hand-rolling the deserialize side.

**Examples:**
- "Is there a reason why you don't derive `BorshDeserialize`? Because this implementation looks a lot like what borsh's deserialize does (especially since you derive `BorshSerialize`)" — on `CreateGeoProbeArgs`. [malbeclabs/doublezero#2961](https://github.com/malbeclabs/doublezero/pull/2961)
- "If you derive either borsh deserialize traits, you wouldn't have to deserialize each element like this" — on `GeolocationProgramConfig`. [malbeclabs/doublezero#3083](https://github.com/malbeclabs/doublezero/pull/3083)
- "How is this different from `BorshDeserialize::deserialize(&mut data[..])`? ... you could do something without this method... FooV1::deserialize(&mut &buf[..]).expect(\"should ignore extra fields\")" — on a `compat_deserialize` helper. [malbeclabs/doublezero#1370](https://github.com/malbeclabs/doublezero/pull/1370)
- "These don't seem necessary since they're light wrappers around the borsh methods" — on `pub fn pack(&self) -> Vec<u8> { borsh::to_vec(&self).unwrap() }`. [malbeclabs/doublezero#3083](https://github.com/malbeclabs/doublezero/pull/3083)

---

### PDA seeds + bump checks

**Current guidance (as of 2026-02-17):** Don't store a bump seed unless a PDA actually signs a CPI (CLAUDE.md Code Org §2) — if an account never signs, storing the bump is unnecessary. Prefer encoding identity into the PDA seeds (e.g. add the collector key as a seed) rather than storing an authority elsewhere, so anyone can write but only the canonical key's data is trusted downstream. Tie PDA labels to the instruction for auditability (name the created PDA `access_pass_account`). For deterministic create-account flows, prefer create-account-with-seed via allocate + assign + transfer to avoid the DoS where lamports pre-sent to the derived key make the runtime believe the account already exists. Avoid calling `find_program_address` twice — conditionally vary the seeds instead.

**Why it matters:** Storing an unused bump wastes account space and misleads readers into thinking the PDA signs. Deriving twice doubles compute. Encoding identity in seeds removes a whole class of authority-validation code. The pre-funded-key DoS can block a create flow entirely.

**What to look for in a diff:**
- A stored `bump`/`bump_seed` on an account that never signs a CPI.
- Two `find_program_address` calls in one path that differ only by a seed value.
- An authority stored on an account where it could instead be a PDA seed.
- A deterministic create path using plain create-account rather than allocate + assign + transfer.
- A created PDA with a name that does not describe its instruction/role.

**Examples:**
- "Is storing the bump seed necessary? When would this account need to sign for anything?" — on `process_create_geo_probe` storing `bump_seed`. [malbeclabs/doublezero#2961](https://github.com/malbeclabs/doublezero/pull/2961)
- "Bumps are the most useless things in Solana. I'll rant to you about it sometime". [malbeclabs/doublezero#1001](https://github.com/malbeclabs/doublezero/pull/1001)
- "I think refactoring this to just add the collector key as a PDA seed would be a simple fix though. Then we can rip out the serviceability stuff added in this PR and any validation that happens when initializing the samples account". [malbeclabs/doublezero#888](https://github.com/malbeclabs/doublezero/pull/888)
- "To avoid calling `find_program_address` twice, can we instead conditionally pass in the IP seed depending on the allow multiple IPs boolean?". [malbeclabs/doublezero#2260](https://github.com/malbeclabs/doublezero/pull/2260)

---

### Access control & authority

**Current guidance (as of 2026-01-21):** Store an authority where it logically belongs and where it is actually used. Do not put one program's authority in another program's global state — an authority for a program should live in that program's own config/global state, or better yet be encoded into the PDA seeds so anyone can write but only the canonical key's data is trusted downstream. Remove owner checks that carry no meaning (an exchange's owner is irrelevant to interacting with the exchange). Question authorization rules more restrictive than the spec requires — a payer in the foundation allowlist should still be removable from the QA allowlist, and the QA allowlist should be emptyable. See also CLAUDE.md **Permissions** (when migrating an instruction to `authorize()`, keep `AUTHORIZE_GATED_FLAGS`, `legacy_keys_for_flags`, and `check_legacy_any` in sync, and revisit `NON_MIGRATED_SUBSYSTEMS`) and **Resource Allocation** (keep `doublezero resource verify` in sync when a processor allocates/deallocates a `ResourceExtension` field).

**Why it matters:** Authority stored in the wrong program couples unrelated codebases and confuses the trust model. A meaningless owner check gives a false sense of protection while blocking legitimate operations. Over-restrictive rules can lock the system into an unrecoverable allowlist state. A gated flag missing from `AUTHORIZE_GATED_FLAGS` makes `doublezero permission audit` silently understate lockout risk.

**What to look for in a diff:**
- An authority for program A being added to program B's `GlobalState`.
- An owner check on an account where ownership has no bearing on the operation.
- An allowlist add/remove path that cannot reach an expected state (e.g. cannot be emptied, cannot remove an entry present in another list).
- A stored authority that could instead be a PDA seed.
- A new instruction migrated to `authorize()` without the matching updates to `AUTHORIZE_GATED_FLAGS` / `legacy_keys_for_flags` / `check_legacy_any` (CLAUDE.md Permissions).
- A processor that allocates/deallocates a `ResourceExtension` field without the matching `verify_*` update (CLAUDE.md Resource Allocation).

**Examples:**
- "Why isn't the internet latency collector just an authority relevant to the telemetry program? Feels like the telemetry program should have a global state that maintains this instead of maintaining it here... the internet latency collector does nothing on the serviceability program." [malbeclabs/doublezero#888](https://github.com/malbeclabs/doublezero/pull/888)
- "We should remove the owner check for suspend, too. The owner means nothing for any interaction with the exchange account" — on `if exchange.owner != *payer_account.key`. [malbeclabs/doublezero#2266](https://github.com/malbeclabs/doublezero/pull/2266)
- "Why can't the payer be removed from the QA allowlist if he himself is in the foundation allowlist? Feels like this can be removed" — on `process_remove_qa_allowlist_globalconfig`. [malbeclabs/doublezero#2683](https://github.com/malbeclabs/doublezero/pull/2683)
- "the simplest would be to add the collector's key to the PDA, so anyone could conceivably add internet latency data. But practically, only the collector would have enough DZ Ledger gas tokens to add state... the reward calculator would only treat the samples written by the canonical latency collector... as the true data." [malbeclabs/doublezero#888](https://github.com/malbeclabs/doublezero/pull/888)

---

### Rent / account space / realloc / heap caps

**Current guidance (as of 2025-12-02):** Understand the close-vs-realloc distinction precisely. Realloc-to-0 does **not** close an account — to truly close it you must move all lamports out (and change owner). Naming an instruction "close" when it only reallocs to 0 bytes will be flagged. For deterministic-key account creation, use allocate + assign + transfer (create-account-with-seed) to sidestep the attack where lamports pre-funded to the derived key make the runtime reject creation. Name rent/airdrop constants for their purpose (`CONTRIBUTOR_AIRDROP_LAMPORTS`, not `OWNER_RENT_EXEMPTION_LAMPORTS`) and factor magic lamport values into named constants. Consolidate the many near-duplicate account-creation helpers. **Enforce collection caps at the mutating instruction** *(reviewer-curated)*: a `Vec` stored on an account (allowlists, interfaces, parent devices, samples) must have its maximum size enforced at the instruction that grows it — not only in a `validate()` elsewhere. An over-grown vec makes borsh deserialization blow the 32KB program heap and panic *every* instruction that reads the account, bricking it. Every newly grown collection needs a cap at the mutating processor and a test exercising the near-cap boundary.

**Why it matters:** A "close" that only reallocs leaves a funded, program-owned account alive — the caller thinks it is gone. The pre-funded-key attack can permanently block a create flow. Mis-named constants misrepresent what lamports are for; scattered creation helpers drift apart. An uncapped account collection is a latent brick: once it grows past what fits the 32KB heap, no instruction can deserialize the account again.

**What to look for in a diff:**
- An instruction named `close`/`process_close_*` that reallocs to 0 without draining lamports (and reassigning owner).
- A deterministic create path not using allocate + assign + transfer.
- Magic lamport literals inline, or a constant named for the wrong purpose (rent vs airdrop).
- Yet another account-creation helper duplicating existing ones.
- A diff that grows an account-stored `Vec` with no maximum enforced at the mutating instruction (heap-brick risk), or with no near-cap boundary test.

**Examples:**
- "The account won't actually be closed unless you move all the lamports out of the account. Did you only intend to realloc to 0 bytes?" — on `process_close_access_pass`. [malbeclabs/doublezero#1425](https://github.com/malbeclabs/doublezero/pull/1425)
- "someone could send some lamports to a pubkey before someone calls create-account-with-seed. So if the process depended on this instruction, the SVM would revert saying that the account has already been created... Performing allocate + assign + transfer is a way to get around that". [malbeclabs/doublezero#748](https://github.com/malbeclabs/doublezero/pull/748)
- "Don't call it `OWNER_RENT_EXEMPTION_LAMPORTS`. But this should be a constant defined somewhere (maybe `CONTRIBUTOR_AIRDROP_LAMPORTS`)". [malbeclabs/doublezero#1119](https://github.com/malbeclabs/doublezero/pull/1119)
- "How many account creation methods are there? Can we consolidate them?" — on `account_create_with_seed`. [malbeclabs/doublezero#2332](https://github.com/malbeclabs/doublezero/pull/2332)

---

### State & invariants

**Current guidance (as of 2026-03-03):** Question whether an invariant check can ever actually fire and whether a data model is fragile. Ask "when would this ever be true?" for `validate()` branches that upstream instructions already guarantee (a code length already bounded at create; `MAX_PARENT_DEVICES` enforced at add time; a case `find_program_address` would already reject) — and, taken together, whether a `Validate` impl is even needed. Conversely, insist the invariant actually be enforced at the mutating instruction: the add-parent-device processor must enforce the `MAX_PARENT_DEVICES` cap itself; you cannot rely on a `validate` elsewhere. Flag fragile designs: deriving `Default` on an args/state struct risks silent bugs when it is reused with new fields, and a `From<u8>` path can silently misinterpret a future enum value.

**Why it matters:** An invariant that can never fire is dead code that misleads reviewers; an invariant enforced only in `validate()` (not at the mutation) can be bypassed. `Default` on a struct with new fields compiles silently with wrong data. `From<u8>` on an enum turns an unknown future variant into a valid-looking wrong one — a real correctness hazard across builds.

**What to look for in a diff:**
- A `validate()` branch guarding a condition already guaranteed at create/add time.
- A cap/limit checked in `validate()` but not enforced in the mutating instruction processor.
- `#[derive(..., Default)]` on args/state structs that may gain fields.
- `impl From<u8> for SomeEnum` where `TryFrom<u8>` is safer against future variants.

**Examples:**
- "Is this true? I thought you would have to enforce that the parent device count doesn't exceed this const in this instruction processor" — on `process_add_parent_device`. [malbeclabs/doublezero#3151](https://github.com/malbeclabs/doublezero/pull/3151)
- "I'm wondering why this trait even needs to be implemented with any of the accounts you have in this smart contract. What does calling `validate` protect against exactly?" — on `impl Validate for GeoProbe`. [malbeclabs/doublezero#3120](https://github.com/malbeclabs/doublezero/pull/3120)
- "be careful with this struct because you derived Default on it. So if you end up reusing this struct in your client and you have `MigrateArgs::default()` with new args, there won't be a compile error to catch it. So it's good you use `MigrateArgs {}` currently." [malbeclabs/doublezero#2332](https://github.com/malbeclabs/doublezero/pull/2332)
- "This `From<u8>` implementation is common throughout the Serviceability codebase. I feel like this should be `TryFrom<u8>` instead. I want to avoid a situation where we add another access pass type, where an old build... ends up processing a u8 value as `Prepaid` but is actually a new value" — on `impl From<u8> for AccessPassType`. [malbeclabs/doublezero#1272](https://github.com/malbeclabs/doublezero/pull/1272)

---

### Integer overflow / checked arithmetic

**Current guidance (as of 2026-03-10):** Match the arithmetic guard to the real risk. Use `checked_add` / `checked_sub` with `.ok_or(err)` for add/sub that could over/underflow — on counts, seats, and lamports (`checked_sub` when decrementing, rather than `saturating_sub`, which silently clamps an underflow bug). **The converse applies too** and matches this skill's redundant-validation philosophy: question a `checked_*`/guard that can *never* fail ("seems impossible for this `checked_sub` to be `None`, right?"); question `wrapping_add`/`wrapping_*` where wrap-around isn't actually intended; and accept a bare `+= 1` where overflow is physically impossible (a u64 epoch or monotonic index counter). Also scrutinize off-by-one boundary conditions (`users_count` may equal `max_users`, so use `>` not `>=`) and prune redundant guards the inequality already covers (a separate `max_users == 0` check is unnecessary if the inequality already fails). *(The converse directions are reviewer-curated; the mined examples below lean toward adding checks.)*

**Why it matters:** Unchecked arithmetic on seat/user counters can overflow and wrap, defeating a capacity limit; `saturating_sub` hides an underflow bug as a clamp. But reflexive `checked_*` on values an invariant already bounds adds compute and implies a risk that doesn't exist, and `wrapping_*` silently normalizes overflow. Off-by-one comparisons let one extra user in or reject a valid one; redundant guards obscure the real bound.

**What to look for in a diff:**
- Plain `+` / `-` on counts, seats, or lamports where over/underflow is possible.
- `saturating_sub` used to decrement a count that should never underflow (should be `checked_sub`).
- A `checked_*`/guard on a value an invariant already bounds — ask for the overflow scenario, or drop it.
- `wrapping_add`/`wrapping_*` on a counter with no intended wrap-around (prefer `checked_add`).
- `>=` / `<=` where the boundary value is actually valid (or vice versa).
- A separate `== 0` guard beside an inequality that already covers it.

**Examples:**
- "Technically this is an arithmetic overflow" — on `if device.users_count + device.reserved_seats >= device.max_users {`. [malbeclabs/doublezero#3224](https://github.com/malbeclabs/doublezero/pull/3224)
- "Should this be `checked_sub` to be safe?" — on `device.reserved_seats = device.reserved_seats.saturating_sub(remaining);`. [malbeclabs/doublezero#3224](https://github.com/malbeclabs/doublezero/pull/3224)
- "`user_count` can equal `max_users` so this can be simplified to `if self.users_count > self.max_users {`" — on `if self.max_users > 0 && self.users_count >= self.max_users {`. [malbeclabs/doublezero#2259](https://github.com/malbeclabs/doublezero/pull/2259)
- "Also is checking that the max == 0 necessary? I think the inequality would be violated anyway" — on `if device.users_count + new_reserved > device.max_users || device.max_users == 0 {`. [malbeclabs/doublezero#3224](https://github.com/malbeclabs/doublezero/pull/3224)

---

### Error handling & custom errors

**Current guidance (as of 2026-02-24):** CLAUDE.md Error Handling §1–§2 codify the on-chain rules (mark the error enum `#[repr(u32)]` + `as u32` and drop the `Custom(u32)` catch-all; error messages state the expected condition, e.g. "reference_count of {n} > 0", not a bare variant name) — diff-scan below. On top of that: avoid panicky patterns — `unwrap_err` after an assertion panics if the assertion is false. **In client/off-chain code `eyre` is the repo's standard error crate (not `anyhow`); when wrapping or converting a lower-level error, include the underlying error in the message — `eyre!("failed to create user: {e}")`, not `eyre!("failed to create user")` — so logs carry the actual cause** (use `{e}` Display for the readable chain, not `{e:?}`). Prefer `ok_or(eyre!(...))` over `ok_or_else` where the value isn't lazily computed.

**Why it matters:** An opaque error forces integrators to reverse-engineer the enum from a numeric code. A catch-all `Custom` variant plus a hand-maintained `From` impl drifts every time a variant is added. `unwrap_err` on a maybe-true condition is a live panic path in on-chain code. An error wrapper that drops the source `e` throws away the actual cause, leaving an operator with a generic message and no lead.

**What to look for in a diff:**
- Error variants / messages that name the value but not the required condition.
- A `Custom(u32)` catch-all variant and a `From<Error> for ProgramError` that matches variant-by-variant instead of `#[repr(u32)]` + `as u32`.
- `unwrap_err()` following an `assert`/comparison that can be false.
- An `eyre!` / `map_err` / `format!` error that drops the source error instead of interpolating `{e}`.
- `ok_or_else(...)` in helper/client code where `ok_or(eyre!(...))` reads cleaner (value not lazily computed).

**Examples:**
- "If you removed `GeolocationError::Custom`, you could just decorate the enum with `#[repr(u32)]` and simply use `as u32` for this `From` impl. Then you wouldn't have to add to this implementation when you add more error variants". [malbeclabs/doublezero#2961](https://github.com/malbeclabs/doublezero/pull/2961)
- "Perhaps this should say reference count must be zero? Unless an integrator matched the error enum to figure out what this means, the condition should be clear on this revert. Right now, if I got this error, I'm not sure what the reference count is supposed to be" — on `process_delete_geo_probe`. [malbeclabs/doublezero#2961](https://github.com/malbeclabs/doublezero/pull/2961)
- "`unwrap_err` below will panic if this line were false". [malbeclabs/doublezero#3083](https://github.com/malbeclabs/doublezero/pull/3083)
- "could you replace all of the `ok_or_else` calls with just `ok_or`? I think you can just use the `anyhow!` macro, too." *(older PR — the repo standard is now `eyre`; the point about `ok_or` vs `ok_or_else` stands.)* [malbeclabs/doublezero#729](https://github.com/malbeclabs/doublezero/pull/729)

---

### CPI safety & RPC / client interaction

**Current guidance (as of 2025-07):** For client/off-chain RPC code (e.g. the payment tracker), do not swallow errors from RPC calls — if a `get_block` call errors, retry the fetch rather than filtering the failed block out, or a transient RPC error silently drops data. Push `RpcClient` in as a reference to each method rather than storing it on a struct. Remove thin wrapper methods that just re-expose an RPC client method (`get_epoch_info`, `get_blocks`) — they add latency and no value. When fetching per-validator data, fetch the whole leader schedule once and index into it by a slice of validator ids instead of one RPC call per validator, and sum lamports only where `reward_type == RewardType::Fee`. **Additional off-chain transaction/RPC hygiene** *(reviewer-curated)*: never `unwrap()`/`expect()` on RPC-derived data — a network blip becomes a process panic; `?`-propagate in `Result`-returning functions. Fetch the recent blockhash inside the send method, not as a staleable argument. Prefer `get_multiple_accounts` over N single fetches. Do pre-flight existence checks (account/ATA) and add the create instruction only when needed, rather than letting the tx revert (a bare revert is poor UX). Don't send zero-value/no-op transactions. Don't set a CU *price* on uncontended DZ-ledger transactions; build the CU *limit* from real measured costs with some headroom.

**Why it matters:** Silently filtering a failed RPC call drops real data and skews aggregates (block rewards) without any signal. Per-validator RPC calls are slow and wasteful. An `unwrap` on RPC data turns a transient network error into a crashed process. A staleable blockhash argument leads to `BlockhashNotFound` preflight failures. Zero-value and always-create transactions waste fees and read as bugs.

**What to look for in a diff:**
- A `get_block`/RPC error handled by filtering/skipping instead of retrying.
- An `RpcClient` stored on a struct rather than passed by reference per call.
- A wrapper method that only forwards to an RpcClient method.
- A per-validator loop issuing one RPC call each instead of one leader-schedule fetch indexed by validator ids.
- Reward summation that does not filter on `reward_type == RewardType::Fee`.
- `.unwrap()`/`.expect()` on an RPC response inside a `Result`-returning function (use `?`).
- A recent blockhash passed in as an argument instead of fetched inside the send method.
- N single-account fetches where `get_multiple_accounts` fits.
- A create instruction always added with no pre-flight existence check.
- A transaction sent for a zero-value/no-op change.
- A CU price set on an uncontended DZ-ledger tx, or a flat CU limit not derived from real costs.

**Examples:**
- "I'm not sure we want to handle any error from `get_block` this way. If we have an RPC error for any of the get block calls, we'd want to retry the fetch and not just filter it out." [malbeclabs/doublezero#729](https://github.com/malbeclabs/doublezero/pull/729)
- "I would rather we get rid of this and pass in a reference to `RpcClient` for each of the methods you implemented." [malbeclabs/doublezero#729](https://github.com/malbeclabs/doublezero/pull/729)
- "can the method just fetch the whole leader schedule and index into the leader schedule based on a slice of `validator_ids`... it makes more sense for the values of this HashMap to be the sum of lamports in the Reward only if the `reward_type` is `RewardType::Fee`." [malbeclabs/doublezero#729](https://github.com/malbeclabs/doublezero/pull/729)
- "We should remove this method. First, it's slow as hell getting a response. Second, it's just a light wrapper around the client's `get_blocks` method." [malbeclabs/doublezero#729](https://github.com/malbeclabs/doublezero/pull/729)

---

### Documentation & changelog

**Current guidance (as of 2026-02-27):** Document non-obvious operational scenarios and keep the changelog current. Write documentation for edge-case operational flows (e.g. the agent-key rollover window across an epoch, so contributors know when to migrate keys). CI now enforces a changelog check (added Dec 2025) — PRs must update the changelog. Durable conventions (borsh deserialization, testing standards) are captured directly into `CLAUDE.md` so they are enforced going forward.

**Why it matters:** Undocumented operational timing (key rollover across an epoch) leads contributors to migrate at the wrong moment and lose access. A stale changelog fails CI and loses the record of behavioral changes.

**What to look for in a diff:**
- A behavioral change with no changelog entry (CI will flag it).
- A new operational edge case (timing windows, migration steps) with no accompanying docs.
- A new durable convention that belongs in `smartcontract/programs/CLAUDE.md`.

**Examples:**
- "Looks like we added a changelog check to CI since you've added this change. Could you update the changelog?" [malbeclabs/doublezero#2318](https://github.com/malbeclabs/doublezero/pull/2318)
- "We should write documentation outlining this scenario, too, just so a contributor knows when he should consider migrating to a new agent key if he has to do so." — on `write_internet_latency_samples.rs`. [malbeclabs/doublezero#888](https://github.com/malbeclabs/doublezero/pull/888)
- "[CLAUDE.md] Assert specific errors: Tests should assert specific error types (e.g., `ProgramError::Custom(17)`), not just `.is_err()`." [malbeclabs/doublezero#3120](https://github.com/malbeclabs/doublezero/pull/3120)

---

### Events / logging & debug representations

**Current guidance (as of 2025-12-02):** Make debug output actually useful, or omit it. For instruction-arg structs, prefer deriving `Debug` (which shows field contents) over a custom `Display` that only prints a length — a bare len does not aid debugging — and if the args are never debugged, implement neither. Do not add `msg!`/print calls purely to suppress an unused-variable warning.

**Why it matters:** A `Display` that prints only a length gives nothing useful in logs while implying the struct is debuggable. Log lines added to silence warnings are noise that will mislead future readers.

**What to look for in a diff:**
- A hand-written `Display` on an args struct that prints only a count/length.
- `Debug`/`Display` impls on structs that are never logged.
- `msg!`/print statements whose only purpose is silencing an unused-variable warning.

**Examples:**
- "maybe this should implement Display instead? Just showing the len doesn't really lend itself to fruitful debugging I think. But if we don't even plan on debugging these instruction arguments, maybe we don't even implement either Debug or Display." — on `WriteInternetLatencySamplesArgs`. [malbeclabs/doublezero#888](https://github.com/malbeclabs/doublezero/pull/888)
- "I figured deriving Debug for those structs would be sufficient (and provide more information than just the len)." [malbeclabs/doublezero#888](https://github.com/malbeclabs/doublezero/pull/888)
- "This feels like an anti-pattern of printing/logging a variable meant to avoid the unused variable warning." — on `process_migrate`. [malbeclabs/doublezero#2332](https://github.com/malbeclabs/doublezero/pull/2332)

---

### Dependencies & build hygiene

**Current guidance (as of 2026-02-24):** Centralize dependencies and reuse existing crates rather than reimplementing. New crate deps should be added to the workspace `Cargo.toml` (workspace-inherited) instead of being pinned per-package. Reuse published Solana crates instead of reimplementing framework functionality — e.g. parse the upgradeable loader state via `solana-loader-v3-interface`'s `UpgradeableLoaderState` rather than hand-parsing `ProgramData` bytes (CLAUDE.md Program Upgrades).

**Why it matters:** Per-package version pins drift out of sync and cause duplicate/incompatible builds. Hand-parsing framework byte layouts is error-prone and breaks when the upstream format changes.

**What to look for in a diff:**
- A new dependency pinned in a package `Cargo.toml` rather than inherited from the workspace.
- Hand-rolled parsing of a well-known Solana structure that a published crate already models.

**Examples:**
- "Can't you just use ...solana_loader_v3_interface/state/enum.UpgradeableLoaderState.html? Seems crazy to implement this yourself" — on a hand-written `parse_upgrade_authority`. [malbeclabs/doublezero#2961](https://github.com/malbeclabs/doublezero/pull/2961)
- "Do you want to add these to the workspace Cargo.toml?" — on per-package `solana-bincode` / `solana-loader-v3-interface` pins. [malbeclabs/doublezero#3083](https://github.com/malbeclabs/doublezero/pull/3083)
- "Should we put this in the workspace dependencies?" — on `solana-transaction-status-client-types`. [malbeclabs/doublezero#729](https://github.com/malbeclabs/doublezero/pull/729)
