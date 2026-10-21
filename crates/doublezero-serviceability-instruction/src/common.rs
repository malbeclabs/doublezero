//! The anti-drift keystone.
//!
//! [`build`] is the single place that appends the trailing accounts every
//! serviceability instruction shares, so the payer/system convention lives in
//! exactly one reviewable location instead of being re-encoded per builder.

use doublezero_serviceability::{instructions::DoubleZeroInstruction, pda::get_globalstate_pda};
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

/// Assemble an instruction whose processor routes through `authorize()`: the
/// instruction-specific metas, the shared trailing `[payer, system]`, and then the
/// payer's Permission PDA as a read-only account (RFC-26 "Permission account").
///
/// The PDA is derived from the payer, never taken as a caller-supplied argument,
/// and it is appended **unconditionally**: a pure builder cannot know whether the
/// account exists onchain, and it does not need to. `authorize()` treats the
/// payer's PDA while it is still uninitialized (system-owned, no data, whatever its
/// lamports) exactly like no Permission account at all, so a payer without one is
/// authorized by the legacy `GlobalState` allowlists while
/// `RequirePermissionAccounts` is off, as before.
///
/// **Deploy ordering.** That tolerance is a program change: older serviceability
/// programs fail an uninitialized trailing PDA with `InvalidAccountData`, because
/// their ownership check runs before the legacy fallback. The program carrying the
/// tolerance must therefore be deployed on a cluster before clients built from this
/// crate are shipped against it.
///
/// The Permission account MUST be appended **last** (before only the RFC-27
/// Instructions sysvar, which [`append_instructions_sysvar`] places after it):
/// `authorize()` reads it as the final account and identifies it by PDA match
/// (`get_permission_pda(payer)`) — so the variable-length / length-detected
/// families tolerate it (the feed / tenant slots sit ahead of the payer and are
/// never confused with it).
pub(crate) fn build_with_permission(
    program_id: &Pubkey,
    instruction: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    payer: &Pubkey,
) -> Instruction {
    build_with_permission_after_system(program_id, instruction, accounts, Vec::new(), payer)
}

/// [`build_with_permission`] for an instruction that also carries accounts *after*
/// `[payer, system]` (the feed bond mirror, which its processors find by address).
/// Those land between the system program and the Permission PDA, so the Permission
/// account stays last.
pub(crate) fn build_with_permission_after_system(
    program_id: &Pubkey,
    instruction: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    after_system: Vec<AccountMeta>,
    payer: &Pubkey,
) -> Instruction {
    let mut ix = build(program_id, instruction, accounts, payer);
    ix.accounts.extend(after_system);
    let (permission, _) = doublezero_serviceability::pda::get_permission_pda(program_id, payer);
    ix.accounts
        .push(AccountMeta::new_readonly(permission, false)); // MUST be last
    ix
}

/// Append the RFC-27 Instructions sysvar as the **very last** account.
///
/// Called on the already-built instruction, not from inside [`build`], because it has to land
/// after everything the shared trailing convention appends — including the Permission PDA that
/// [`build_with_permission`] appends. `split_trailing_permission`
/// identifies the Permission account by PDA match and `split_trailing_instructions_sysvar`
/// identifies this one by its fixed key, and the processor peels the sysvar first, so "sysvar
/// last, Permission second-to-last" is the only order both can parse.
pub(crate) fn append_instructions_sysvar(mut instruction: Instruction) -> Instruction {
    instruction.accounts.push(AccountMeta::new_readonly(
        solana_program::sysvar::instructions::ID,
        false,
    ));
    instruction
}

/// Assemble a builder whose only instruction-specific account is GlobalState
/// (writable). Shared by the single-GlobalState setters and allowlist toggles;
/// all route through `authorize()` -> [`build_with_permission`].
pub(crate) fn build_globalstate_only(
    program_id: &Pubkey,
    payer: &Pubkey,
    instruction: DoubleZeroInstruction,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    build_with_permission(
        program_id,
        instruction,
        vec![AccountMeta::new(globalstate, false)],
        payer,
    )
}

/// The read-only payer Permission PDA meta [`build_with_permission`] appends, for
/// asserting builder account layouts in tests.
#[cfg(test)]
pub(crate) fn permission_meta(program_id: &Pubkey, payer: &Pubkey) -> AccountMeta {
    let (permission, _) = doublezero_serviceability::pda::get_permission_pda(program_id, payer);
    AccountMeta::new_readonly(permission, false)
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
