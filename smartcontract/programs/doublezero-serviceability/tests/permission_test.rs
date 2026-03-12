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
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
};

mod test_helpers;
use test_helpers::*;

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
