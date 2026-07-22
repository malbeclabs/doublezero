//! Contributor-domain instruction builders.
//!
//! CRUD template like `location`; `create` also takes the contributor `owner`.
//! All route through `authorize()` -> [`common::build_with_permission`].

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_contributor_pda, get_globalstate_pda},
    processors::contributor::{
        create::ContributorCreateArgs, delete::ContributorDeleteArgs,
        resume::ContributorResumeArgs, suspend::ContributorSuspendArgs,
        update::ContributorUpdateArgs,
    },
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateContributor` (variant 60). Accounts: `[contributor, owner, globalstate]`.
///
/// `account_index` is the new contributor's index (`globalstate.account_index + 1`).
pub fn create_contributor(
    program_id: &Pubkey,
    payer: &Pubkey,
    owner: &Pubkey,
    account_index: u128,
    args: ContributorCreateArgs,
) -> Instruction {
    let (contributor, _) = get_contributor_pda(program_id, account_index);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateContributor(args),
        vec![
            AccountMeta::new(contributor, false),
            AccountMeta::new(*owner, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `UpdateContributor` (variant 61). Accounts: `[contributor, globalstate]`.
pub fn update_contributor(
    program_id: &Pubkey,
    payer: &Pubkey,
    contributor: &Pubkey,
    args: ContributorUpdateArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdateContributor(args),
        vec![
            AccountMeta::new(*contributor, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `SuspendContributor` (variant 62). Accounts: `[contributor, globalstate]`.
pub fn suspend_contributor(
    program_id: &Pubkey,
    payer: &Pubkey,
    contributor: &Pubkey,
    args: ContributorSuspendArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SuspendContributor(args),
        vec![
            AccountMeta::new(*contributor, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `ResumeContributor` (variant 63). Accounts: `[contributor, globalstate]`.
pub fn resume_contributor(
    program_id: &Pubkey,
    payer: &Pubkey,
    contributor: &Pubkey,
    args: ContributorResumeArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::ResumeContributor(args),
        vec![
            AccountMeta::new(*contributor, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `DeleteContributor` (variant 64). Accounts: `[contributor, globalstate]`.
pub fn delete_contributor(
    program_id: &Pubkey,
    payer: &Pubkey,
    contributor: &Pubkey,
    args: ContributorDeleteArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteContributor(args),
        vec![
            AccountMeta::new(*contributor, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_create_contributor_includes_owner() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let ix = create_contributor(&pid, &payer, &owner, 1, ContributorCreateArgs::default());
        assert_eq!(ix.data[0], 60);
        let (contributor, _) = get_contributor_pda(&pid, 1);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(contributor, false),
                AccountMeta::new(owner, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_contributor_pubkey_verbs() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);
        let expected = vec![
            AccountMeta::new(contributor, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(payer, true),
            AccountMeta::new(system_program::ID, false),
        ];
        for (ix, tag) in [
            (
                update_contributor(&pid, &payer, &contributor, ContributorUpdateArgs::default()),
                61,
            ),
            (
                suspend_contributor(&pid, &payer, &contributor, ContributorSuspendArgs {}),
                62,
            ),
            (
                resume_contributor(&pid, &payer, &contributor, ContributorResumeArgs {}),
                63,
            ),
            (
                delete_contributor(&pid, &payer, &contributor, ContributorDeleteArgs {}),
                64,
            ),
        ] {
            assert_eq!(ix.data[0], tag);
            assert_eq!(ix.accounts, expected);
        }
    }
}
