//! Allowlist-domain instruction builders (foundation + QA).
//!
//! Each toggles a single pubkey on the GlobalState allowlist; the only account is
//! GlobalState. All route through `authorize()` -> [`common::build_with_permission`].

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_globalstate_pda,
    processors::allowlist::{
        foundation::{add::AddFoundationAllowlistArgs, remove::RemoveFoundationAllowlistArgs},
        qa::{add::AddQaAllowlistArgs, remove::RemoveQaAllowlistArgs},
    },
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

fn build_globalstate_only(
    program_id: &Pubkey,
    payer: &Pubkey,
    instruction: DoubleZeroInstruction,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        instruction,
        vec![AccountMeta::new(globalstate, false)],
        payer,
    )
}

/// `AddFoundationAllowlist` (variant 4). Accounts: `[globalstate]`.
pub fn add_foundation_allowlist(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: AddFoundationAllowlistArgs,
) -> Instruction {
    build_globalstate_only(
        program_id,
        payer,
        DoubleZeroInstruction::AddFoundationAllowlist(args),
    )
}

/// `RemoveFoundationAllowlist` (variant 5). Accounts: `[globalstate]`.
pub fn remove_foundation_allowlist(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: RemoveFoundationAllowlistArgs,
) -> Instruction {
    build_globalstate_only(
        program_id,
        payer,
        DoubleZeroInstruction::RemoveFoundationAllowlist(args),
    )
}

/// `AddQaAllowlist` (variant 86). Accounts: `[globalstate]`.
pub fn add_qa_allowlist(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: AddQaAllowlistArgs,
) -> Instruction {
    build_globalstate_only(
        program_id,
        payer,
        DoubleZeroInstruction::AddQaAllowlist(args),
    )
}

/// `RemoveQaAllowlist` (variant 87). Accounts: `[globalstate]`.
pub fn remove_qa_allowlist(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: RemoveQaAllowlistArgs,
) -> Instruction {
    build_globalstate_only(
        program_id,
        payer,
        DoubleZeroInstruction::RemoveQaAllowlist(args),
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_allowlist_toggles() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);
        let expected = vec![
            AccountMeta::new(globalstate, false),
            AccountMeta::new(payer, true),
            AccountMeta::new(system_program::ID, false),
        ];
        for (ix, tag) in [
            (
                add_foundation_allowlist(&pid, &payer, AddFoundationAllowlistArgs::default()),
                4,
            ),
            (
                remove_foundation_allowlist(&pid, &payer, RemoveFoundationAllowlistArgs::default()),
                5,
            ),
            (
                add_qa_allowlist(&pid, &payer, AddQaAllowlistArgs::default()),
                86,
            ),
            (
                remove_qa_allowlist(&pid, &payer, RemoveQaAllowlistArgs::default()),
                87,
            ),
        ] {
            assert_eq!(ix.data[0], tag);
            assert_eq!(ix.accounts, expected);
        }
    }
}
