//! The anti-drift keystone.
//!
//! [`build`] is the single place that appends the trailing accounts every
//! serviceability instruction shares, so the payer/system convention lives in
//! exactly one reviewable location instead of being re-encoded per builder.

use doublezero_serviceability::instructions::DoubleZeroInstruction;
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// Protocol-max compute-unit limit for a serviceability transaction. Mirrors
/// `sdk/rs/src/client.rs::MAX_COMPUTE_UNIT_LIMIT`.
pub const MAX_COMPUTE_UNIT_LIMIT: u32 = 1_400_000;
/// Protocol-max heap-frame request (256 KiB). Mirrors
/// `sdk/rs/src/client.rs::MAX_HEAP_FRAME_BYTES`.
pub const MAX_HEAP_FRAME_BYTES: u32 = 256 * 1024;

/// Assemble a single serviceability [`Instruction`] from its instruction-specific
/// account metas (in processor order, WITHOUT payer/system) plus the shared
/// trailing `[payer, system]`.
///
/// This is the permanent **no-permission** path: instructions that never route
/// through `authorize()` (e.g. `CreateUser`) call this. Instructions that do
/// route through `authorize()` call [`build_with_permission`] instead.
///
/// The wire encoding is `1 tag byte + borsh(args)`, obtained for free from
/// [`DoubleZeroInstruction::pack`] — builders never hand-write tag bytes.
pub(crate) fn build(
    program_id: &Pubkey,
    instruction: DoubleZeroInstruction,
    mut accounts: Vec<AccountMeta>,
    payer: &Pubkey,
) -> Instruction {
    accounts.push(AccountMeta::new(*payer, true));
    // The system program is marked **writable** for byte-parity with today's
    // `client.rs::assemble_instructions` (`AccountMeta::new`); the runtime demotes
    // reserved keys, so this is harmless. Byte-parity is deliberate — the golden
    // fixtures freeze this flag, and diverging from the SDK would defeat the
    // drop-in-replacement goal. The RFC glossary is aligned to match.
    accounts.push(AccountMeta::new(
        solana_system_interface::program::ID,
        false,
    ));

    Instruction::new_with_bytes(*program_id, &instruction.pack(), accounts)
}

/// Assemble an instruction whose processor routes through `authorize()` and will
/// therefore carry a trailing, read-only Permission PDA (derived from the payer,
/// never a caller-supplied argument) once the permission model is rolled out.
///
/// Builders are **assigned** to this method per-instruction as they are
/// implemented; the append itself is **deferred** (RFC-26 "Permission account").
/// Today it simply delegates to [`build`], so assigning a builder here is
/// behavior-preserving. At the permission rollout, enable the append **here, in
/// this one place** — it then activates for every builder already assigned to
/// this method at once, while builders on [`build`] stay untouched.
///
/// The Permission account MUST be appended **last**: `authorize()` reads it as
/// the final account and identifies it by PDA match (`get_permission_pda(payer)`)
/// — so the variable-length / length-detected families tolerate it (the feed /
/// tenant slots sit ahead of the payer and are never confused with it). To
/// activate, assemble here instead of delegating:
///
/// ```ignore
/// accounts.push(AccountMeta::new(*payer, true));
/// accounts.push(AccountMeta::new(solana_system_interface::program::ID, false));
/// let (permission, _) =
///     doublezero_serviceability::pda::get_permission_pda(program_id, payer);
/// accounts.push(AccountMeta::new_readonly(permission, false)); // MUST be last
/// Instruction::new_with_bytes(*program_id, &instruction.pack(), accounts)
/// ```
///
/// **Activation precondition.** This append is a pure, offline operation: it
/// cannot check whether the payer's Permission PDA actually exists onchain. When
/// the account does not exist, `authorize()` fails **hard** with
/// `InvalidAccountData` — the program-ownership check runs before the
/// foundation-recovery/legacy fallback (see `authorize.rs`), so a missing
/// Permission account is never routed to the legacy allowlist path. Today's SDK
/// sidesteps this by appending the PDA only after an RPC existence check, which a
/// pure builder cannot replicate. The append MUST therefore stay deferred until
/// every payer is guaranteed a Permission account (e.g. `RequirePermissionAccounts`
/// enforced), or activating it would break every payer without one.
pub(crate) fn build_with_permission(
    program_id: &Pubkey,
    instruction: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    payer: &Pubkey,
) -> Instruction {
    build(program_id, instruction, accounts, payer)
}

/// The transaction-level compute-budget prelude: set the compute-unit limit and
/// request the heap frame. This is NOT inside each builder — the caller prepends
/// it once per transaction over the built instruction(s).
pub fn compute_budget_prelude() -> [Instruction; 2] {
    [
        ComputeBudgetInstruction::set_compute_unit_limit(MAX_COMPUTE_UNIT_LIMIT),
        ComputeBudgetInstruction::request_heap_frame(MAX_HEAP_FRAME_BYTES),
    ]
}
