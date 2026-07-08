use doublezero_serviceability::{
    instructions::*,
    pda::get_permission_pda,
    processors::permission::{
        create::PermissionCreateArgs, delete::PermissionDeleteArgs, resume::PermissionResumeArgs,
        suspend::PermissionSuspendArgs, update::PermissionUpdateArgs,
    },
    state::{
        accounttype::AccountType,
        permission::{permission_flags, PermissionStatus},
    },
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

// DoubleZeroError::InvalidArgument maps to ProgramError::Custom(65).
const INVALID_ARGUMENT: u32 = 65;

/// Assert that a failed transaction reverted with the expected program error code.
/// Repo convention (programs/CLAUDE.md): assert the specific error, not just `is_err()`,
/// so a regression that fails for the wrong reason is caught.
fn assert_custom_error(result: Result<(), BanksClientError>, expected_code: u32) {
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        ))) if code == expected_code => {}
        other => panic!("expected Custom({expected_code}), got {other:?}"),
    }
}

async fn get_permission(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    user_payer: &Pubkey,
) -> doublezero_serviceability::state::permission::Permission {
    let (pda, _) = get_permission_pda(&program_id, user_payer);
    get_account_data(banks_client, pda)
        .await
        .expect("Permission account not found")
        .get_permission()
        .expect("Not a Permission account")
}

#[tokio::test]
async fn test_permission_crud() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    println!("1. Create Permission with USER_ADMIN flag");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let permission = get_permission(&mut banks_client, program_id, &user_payer).await;
    assert_eq!(permission.account_type, AccountType::Permission);
    assert_eq!(permission.status, PermissionStatus::Activated);
    assert_eq!(permission.user_payer, user_payer);
    assert_eq!(permission.permissions, permission_flags::USER_ADMIN);

    println!("2. Update Permission to add NETWORK_ADMIN");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdatePermission(PermissionUpdateArgs {
            add: permission_flags::NETWORK_ADMIN,
            remove: 0,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let permission = get_permission(&mut banks_client, program_id, &user_payer).await;
    assert_eq!(
        permission.permissions,
        permission_flags::USER_ADMIN | permission_flags::NETWORK_ADMIN
    );

    println!("3. Suspend Permission");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SuspendPermission(PermissionSuspendArgs {}),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let permission = get_permission(&mut banks_client, program_id, &user_payer).await;
    assert_eq!(permission.status, PermissionStatus::Suspended);

    println!("4. Resume Permission");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ResumePermission(PermissionResumeArgs {}),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let permission = get_permission(&mut banks_client, program_id, &user_payer).await;
    assert_eq!(permission.status, PermissionStatus::Activated);

    println!("5. Delete Permission");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeletePermission(PermissionDeleteArgs {}),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let account = banks_client.get_account(permission_pda).await.unwrap();
    assert!(account.is_none(), "Permission account should be closed");
}

#[tokio::test]
async fn test_permission_create_requires_foundation() {
    let (mut banks_client, _payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let unauthorized = Keypair::new();
    transfer(
        &mut banks_client,
        &_payer,
        &unauthorized.pubkey(),
        10_000_000,
    )
    .await;

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &unauthorized,
    )
    .await;

    assert!(
        result.is_err(),
        "Unauthorized payer should not be able to create permissions"
    );
}

#[tokio::test]
async fn test_permission_create_zero_flags_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: 0,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err(), "Zero permissions should be rejected");
}

#[tokio::test]
async fn test_permission_double_create_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::FOUNDATION,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Use different permissions so the second transaction has a distinct signature.
    let recent_blockhash2 = banks_client.get_latest_blockhash().await.unwrap();

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash2,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::NETWORK_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err(), "Creating a permission twice should fail");
}

#[tokio::test]
async fn test_permission_suspend_already_suspended_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::ACTIVATOR,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SuspendPermission(PermissionSuspendArgs {}),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SuspendPermission(PermissionSuspendArgs {}),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "Suspending an already-suspended permission should fail"
    );
}

#[tokio::test]
async fn test_permission_resume_active_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::SENTINEL,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ResumePermission(PermissionResumeArgs {}),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err(), "Resuming an active permission should fail");
}

#[tokio::test]
async fn test_permission_globalstate_admin_flag() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::GLOBALSTATE_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let permission = get_permission(&mut banks_client, program_id, &user_payer).await;
    assert_eq!(permission.permissions, permission_flags::GLOBALSTATE_ADMIN);
    assert_eq!(permission.status, PermissionStatus::Activated);
}

#[tokio::test]
async fn test_permission_contributor_admin_flag() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::CONTRIBUTOR_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let permission = get_permission(&mut banks_client, program_id, &user_payer).await;
    assert_eq!(permission.permissions, permission_flags::CONTRIBUTOR_ADMIN);
    assert_eq!(permission.status, PermissionStatus::Activated);
}

#[tokio::test]
async fn test_permission_combined_tier1_flags() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    let all_tier1 = permission_flags::PERMISSION_ADMIN
        | permission_flags::GLOBALSTATE_ADMIN
        | permission_flags::CONTRIBUTOR_ADMIN;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: all_tier1,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let permission = get_permission(&mut banks_client, program_id, &user_payer).await;
    assert_eq!(permission.permissions, all_tier1);
}

#[tokio::test]
async fn test_update_permission_enable_only() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash2 = banks_client.get_latest_blockhash().await.unwrap();

    execute_transaction(
        &mut banks_client,
        recent_blockhash2,
        program_id,
        DoubleZeroInstruction::UpdatePermission(PermissionUpdateArgs {
            add: permission_flags::NETWORK_ADMIN,
            remove: 0,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let permission = get_permission(&mut banks_client, program_id, &user_payer).await;
    assert_eq!(
        permission.permissions,
        permission_flags::USER_ADMIN | permission_flags::NETWORK_ADMIN
    );
}

#[tokio::test]
async fn test_update_permission_disable_only() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::USER_ADMIN | permission_flags::NETWORK_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash2 = banks_client.get_latest_blockhash().await.unwrap();

    execute_transaction(
        &mut banks_client,
        recent_blockhash2,
        program_id,
        DoubleZeroInstruction::UpdatePermission(PermissionUpdateArgs {
            add: 0,
            remove: permission_flags::NETWORK_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let permission = get_permission(&mut banks_client, program_id, &user_payer).await;
    assert_eq!(permission.permissions, permission_flags::USER_ADMIN);
}

#[tokio::test]
async fn test_update_permission_overlap_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash2 = banks_client.get_latest_blockhash().await.unwrap();

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash2,
        program_id,
        DoubleZeroInstruction::UpdatePermission(PermissionUpdateArgs {
            add: permission_flags::NETWORK_ADMIN,
            remove: permission_flags::NETWORK_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "Overlapping add and remove bits should be rejected"
    );
}

#[tokio::test]
async fn test_update_permission_noop_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash2 = banks_client.get_latest_blockhash().await.unwrap();

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash2,
        program_id,
        DoubleZeroInstruction::UpdatePermission(PermissionUpdateArgs { add: 0, remove: 0 }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "No-op update (add=0, remove=0) should be rejected"
    );
}

#[tokio::test]
async fn test_delete_self_removal_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create a permission for the payer itself (the foundation key).
    let (permission_pda, _) = get_permission_pda(&program_id, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: payer.pubkey(),
            permissions: permission_flags::PERMISSION_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash2 = banks_client.get_latest_blockhash().await.unwrap();

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash2,
        program_id,
        DoubleZeroInstruction::DeletePermission(PermissionDeleteArgs {}),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "Caller should not be able to delete their own permission"
    );
}

#[tokio::test]
async fn test_suspend_self_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create a permission for the payer itself (the foundation key).
    let (permission_pda, _) = get_permission_pda(&program_id, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: payer.pubkey(),
            permissions: permission_flags::PERMISSION_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash2 = banks_client.get_latest_blockhash().await.unwrap();

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash2,
        program_id,
        DoubleZeroInstruction::SuspendPermission(PermissionSuspendArgs {}),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Only the self-suspension guard can reject here (the permission is Activated and the
    // caller is otherwise authorized), so the specific error pins that guard.
    assert_custom_error(result, INVALID_ARGUMENT);
}

#[tokio::test]
async fn test_suspend_self_rejected_non_foundation_admin() {
    // Realistic #3996 scenario: a non-foundation PERMISSION_ADMIN whose authorization
    // comes purely from its own Permission PDA (the SDK appends it as the trailing
    // account) tries to suspend itself. `authorize()` WOULD accept it — so this pins that
    // the self-guard runs *before* authorize() and its placement is load-bearing.
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let (admin, admin_perm_pda) = grant_permission_admin(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        permission_flags::PERMISSION_ADMIN,
    )
    .await;

    let rb = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction_with_extra_accounts(
        &mut banks_client,
        rb,
        program_id,
        DoubleZeroInstruction::SuspendPermission(PermissionSuspendArgs {}),
        vec![
            AccountMeta::new(admin_perm_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &admin,
        &[AccountMeta::new_readonly(admin_perm_pda, false)],
    )
    .await;

    assert_custom_error(result, INVALID_ARGUMENT);
}

#[tokio::test]
async fn test_update_self_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create a permission for the payer itself (the foundation key) with TWO flags. This
    // isolates the self-modification guard from the `permissions == 0` guard: removing a
    // single non-critical flag leaves PERMISSION_ADMIN set (permissions != 0), so if the
    // self-guard were removed the update would SUCCEED. A single-flag setup would instead
    // be caught by the `permissions == 0` guard with the identical error, making the test
    // unable to detect regression of the guard it is meant to cover.
    let (permission_pda, _) = get_permission_pda(&program_id, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: payer.pubkey(),
            permissions: permission_flags::PERMISSION_ADMIN | permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash2 = banks_client.get_latest_blockhash().await.unwrap();

    // Removing only USER_ADMIN from one's own permission (PERMISSION_ADMIN stays set, so
    // permissions != 0) must still be rejected by the blanket self-modification guard.
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash2,
        program_id,
        DoubleZeroInstruction::UpdatePermission(PermissionUpdateArgs {
            add: 0,
            remove: permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert_custom_error(result, INVALID_ARGUMENT);
}

#[tokio::test]
async fn test_update_permission_to_zero_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Permission for a third party (not the signer), so the self-modification guard
    // does not fire and we isolate the "must grant at least one flag" invariant.
    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash2 = banks_client.get_latest_blockhash().await.unwrap();

    // Removing the only flag would leave an Activated permission granting nothing.
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash2,
        program_id,
        DoubleZeroInstruction::UpdatePermission(PermissionUpdateArgs {
            add: 0,
            remove: permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // The third-party target dodges the self-modification guard, so the only rejection
    // path is the "must grant at least one defined flag" invariant — the specific error
    // pins it, and the account must be left untouched.
    assert_custom_error(result, INVALID_ARGUMENT);
    let perm = get_permission(&mut banks_client, program_id, &user_payer).await;
    assert_eq!(perm.permissions, permission_flags::USER_ADMIN);
}

#[tokio::test]
async fn test_update_permission_undefined_bits_only_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Third-party target (avoids the self-modification guard) so we isolate the
    // ALL_FLAGS mask: a value made only of undefined bits grants nothing real.
    let user_payer = Pubkey::new_unique();
    let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer,
            permissions: permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash2 = banks_client.get_latest_blockhash().await.unwrap();

    // Add an undefined bit (1 << 127) while removing the only defined flag: `permissions`
    // is non-zero afterward, so a bare `!= 0` check would accept it, but `& ALL_FLAGS`
    // must reject it since no authorize() check could ever match.
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash2,
        program_id,
        DoubleZeroInstruction::UpdatePermission(PermissionUpdateArgs {
            add: 1 << 127,
            remove: permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert_custom_error(result, INVALID_ARGUMENT);
    let perm = get_permission(&mut banks_client, program_id, &user_payer).await;
    assert_eq!(perm.permissions, permission_flags::USER_ADMIN);
}

// Grants admin a plain PERMISSION_ADMIN permission and returns (admin keypair, its PDA).
async fn grant_permission_admin(
    banks_client: &mut BanksClient,
    payer: &Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    flags: u128,
) -> (Keypair, Pubkey) {
    let admin = Keypair::new();
    transfer(banks_client, payer, &admin.pubkey(), 100_000_000).await;
    let (admin_perm_pda, _) = get_permission_pda(&program_id, &admin.pubkey());
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: admin.pubkey(),
            permissions: flags,
        }),
        vec![
            AccountMeta::new(admin_perm_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;
    (admin, admin_perm_pda)
}

#[tokio::test]
async fn test_permission_admin_cannot_grant_foundation_on_create() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    // Foundation grants `admin` a plain PERMISSION_ADMIN (no FOUNDATION flag).
    let (admin, admin_perm_pda) = grant_permission_admin(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        permission_flags::PERMISSION_ADMIN,
    )
    .await;

    let target = Pubkey::new_unique();
    let (target_pda, _) = get_permission_pda(&program_id, &target);

    // admin (PERMISSION_ADMIN only, not foundation) tries to grant FOUNDATION -> denied.
    let rb = banks_client.get_latest_blockhash().await.unwrap();
    let denied = try_execute_transaction_with_extra_accounts(
        &mut banks_client,
        rb,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: target,
            permissions: permission_flags::FOUNDATION,
        }),
        vec![
            AccountMeta::new(target_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &admin,
        &[AccountMeta::new_readonly(admin_perm_pda, false)],
    )
    .await;
    assert!(
        denied.is_err(),
        "PERMISSION_ADMIN holder must not be able to grant FOUNDATION"
    );

    // Control: the same admin CAN grant a non-FOUNDATION flag on the same PDA.
    let rb2 = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        rb2,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: target,
            permissions: permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(target_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &admin,
        &[AccountMeta::new_readonly(admin_perm_pda, false)],
    )
    .await;
    let perm = get_permission(&mut banks_client, program_id, &target).await;
    assert_eq!(perm.permissions, permission_flags::USER_ADMIN);
}

#[tokio::test]
async fn test_permission_admin_cannot_add_foundation_on_update() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let (admin, admin_perm_pda) = grant_permission_admin(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        permission_flags::PERMISSION_ADMIN,
    )
    .await;

    // Foundation creates a target permission with USER_ADMIN.
    let target = Pubkey::new_unique();
    let (target_pda, _) = get_permission_pda(&program_id, &target);
    let rb = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        rb,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: target,
            permissions: permission_flags::USER_ADMIN,
        }),
        vec![
            AccountMeta::new(target_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // admin tries to ADD FOUNDATION via update -> denied.
    let rb2 = banks_client.get_latest_blockhash().await.unwrap();
    let denied = try_execute_transaction_with_extra_accounts(
        &mut banks_client,
        rb2,
        program_id,
        DoubleZeroInstruction::UpdatePermission(PermissionUpdateArgs {
            add: permission_flags::FOUNDATION,
            remove: 0,
        }),
        vec![
            AccountMeta::new(target_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &admin,
        &[AccountMeta::new_readonly(admin_perm_pda, false)],
    )
    .await;
    assert!(
        denied.is_err(),
        "PERMISSION_ADMIN holder must not be able to add FOUNDATION via update"
    );

    // Control: admin CAN add a non-FOUNDATION flag.
    let rb3 = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        rb3,
        program_id,
        DoubleZeroInstruction::UpdatePermission(PermissionUpdateArgs {
            add: permission_flags::NETWORK_ADMIN,
            remove: 0,
        }),
        vec![
            AccountMeta::new(target_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &admin,
        &[AccountMeta::new_readonly(admin_perm_pda, false)],
    )
    .await;
    let perm = get_permission(&mut banks_client, program_id, &target).await;
    assert_eq!(
        perm.permissions,
        permission_flags::USER_ADMIN | permission_flags::NETWORK_ADMIN
    );
}

#[tokio::test]
async fn test_foundation_flag_holder_can_grant_foundation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    // Only foundation can bootstrap a FOUNDATION-flag holder.
    let (admin, admin_perm_pda) = grant_permission_admin(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        permission_flags::FOUNDATION | permission_flags::PERMISSION_ADMIN,
    )
    .await;

    // admin holds the FOUNDATION flag, so it CAN grant FOUNDATION to a target.
    let target = Pubkey::new_unique();
    let (target_pda, _) = get_permission_pda(&program_id, &target);
    let rb = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        rb,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: target,
            permissions: permission_flags::FOUNDATION,
        }),
        vec![
            AccountMeta::new(target_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &admin,
        &[AccountMeta::new_readonly(admin_perm_pda, false)],
    )
    .await;
    let perm = get_permission(&mut banks_client, program_id, &target).await;
    assert_eq!(perm.permissions, permission_flags::FOUNDATION);
}
