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
