//! Permission-domain instruction builders.
//!
//! All route through `authorize()` (PERMISSION_ADMIN) -> [`common::build_with_permission`].
//! The globalstate account is always read-only here. Note the `permission` account
//! at index 0 is the *target* permission being managed, distinct from the payer's
//! own Permission PDA that `authorize()` reads (once activated).

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_permission_pda},
    processors::permission::{
        create::PermissionCreateArgs, delete::PermissionDeleteArgs, resume::PermissionResumeArgs,
        suspend::PermissionSuspendArgs, update::PermissionUpdateArgs,
    },
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreatePermission` (variant 97). Accounts: `[permission, globalstate(readonly)]`.
///
/// The target permission PDA is derived from `args.user_payer`.
pub fn create_permission(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: PermissionCreateArgs,
) -> Instruction {
    let (permission, _) = get_permission_pda(program_id, &args.user_payer);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreatePermission(args),
        vec![
            AccountMeta::new(permission, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

/// `UpdatePermission` (variant 98). Accounts: `[permission, globalstate(readonly)]`.
pub fn update_permission(
    program_id: &Pubkey,
    payer: &Pubkey,
    permission: &Pubkey,
    args: PermissionUpdateArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdatePermission(args),
        vec![
            AccountMeta::new(*permission, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

/// `SuspendPermission` (variant 99). Accounts: `[permission, globalstate(readonly)]`.
pub fn suspend_permission(
    program_id: &Pubkey,
    payer: &Pubkey,
    permission: &Pubkey,
    args: PermissionSuspendArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SuspendPermission(args),
        vec![
            AccountMeta::new(*permission, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

/// `ResumePermission` (variant 100). Accounts: `[permission, globalstate(readonly)]`.
pub fn resume_permission(
    program_id: &Pubkey,
    payer: &Pubkey,
    permission: &Pubkey,
    args: PermissionResumeArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::ResumePermission(args),
        vec![
            AccountMeta::new(*permission, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

/// `DeletePermission` (variant 101). Accounts: `[permission, globalstate(readonly)]`.
pub fn delete_permission(
    program_id: &Pubkey,
    payer: &Pubkey,
    permission: &Pubkey,
    args: PermissionDeleteArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeletePermission(args),
        vec![
            AccountMeta::new(*permission, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_create_permission_derives_target_from_user_payer() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user_payer = Pubkey::new_unique();
        let args = PermissionCreateArgs {
            user_payer,
            ..Default::default()
        };
        let ix = create_permission(&pid, &payer, args);
        assert_eq!(ix.data[0], 97);
        let (permission, _) = get_permission_pda(&pid, &user_payer);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(permission, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_permission_pubkey_verbs() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let permission = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);
        let expected = vec![
            AccountMeta::new(permission, false),
            AccountMeta::new_readonly(globalstate, false),
            AccountMeta::new(payer, true),
            AccountMeta::new(system_program::ID, false),
        ];
        for (ix, tag) in [
            (
                update_permission(&pid, &payer, &permission, PermissionUpdateArgs::default()),
                98,
            ),
            (
                suspend_permission(&pid, &payer, &permission, PermissionSuspendArgs {}),
                99,
            ),
            (
                resume_permission(&pid, &payer, &permission, PermissionResumeArgs {}),
                100,
            ),
            (
                delete_permission(&pid, &payer, &permission, PermissionDeleteArgs {}),
                101,
            ),
        ] {
            assert_eq!(ix.data[0], tag);
            assert_eq!(ix.accounts, expected);
        }
    }
}
