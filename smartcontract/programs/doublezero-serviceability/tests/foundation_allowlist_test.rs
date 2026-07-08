use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        allowlist::foundation::{
            add::AddFoundationAllowlistArgs, remove::RemoveFoundationAllowlistArgs,
        },
        permission::create::PermissionCreateArgs,
    },
    state::{accounttype::AccountType, permission::permission_flags},
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

// DoubleZeroError::InvalidFoundationAllowlist maps to ProgramError::Custom(66).
const INVALID_FOUNDATION_ALLOWLIST: u32 = 66;

fn assert_custom_error(result: Result<(), BanksClientError>, expected_code: u32) {
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        ))) if code == expected_code => {}
        other => panic!("expected Custom({expected_code}), got {other:?}"),
    }
}

#[tokio::test]
async fn foundation_allowlist_test() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    /***********************************************************************************************************************************/
    println!("🟢  Start foundation_allowlist_test");

    let user1 = Pubkey::new_unique();
    let user2 = Pubkey::new_unique();

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    println!("🟢 1. Global Initialization...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    /*****************************************************************************************************************************************************/
    println!("🟢 2. Add user1 to foundation allowlist...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddFoundationAllowlist(AddFoundationAllowlistArgs { pubkey: user1 }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();

    assert_eq!(state.account_type, AccountType::GlobalState);
    assert_eq!(state.foundation_allowlist.len(), 2);
    assert!(state.foundation_allowlist.contains(&user1));

    println!("✅ Allowlist is correct");
    /*****************************************************************************************************************************************************/
    println!("🟢 3. Add user2 to foundation allowlist...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddFoundationAllowlist(AddFoundationAllowlistArgs { pubkey: user2 }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();

    assert_eq!(state.account_type, AccountType::GlobalState);
    assert_eq!(state.foundation_allowlist.len(), 3);
    assert!(state.foundation_allowlist.contains(&user1));
    assert!(state.foundation_allowlist.contains(&user2));

    println!("✅ Allowlist is correct");
    /*****************************************************************************************************************************************************/
    println!("🟢 4. Remove user1 to foundation allowlist...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveFoundationAllowlist(RemoveFoundationAllowlistArgs {
            pubkey: user1,
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();

    assert_eq!(state.account_type, AccountType::GlobalState);
    assert_eq!(state.foundation_allowlist.len(), 2);
    assert!(!state.foundation_allowlist.contains(&user1));
    assert!(state.foundation_allowlist.contains(&user2));

    println!("✅ Allowlist is correct");
    /*****************************************************************************************************************************************************/
    println!("🟢 5. Remove user2 to foundation allowlist...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveFoundationAllowlist(RemoveFoundationAllowlistArgs {
            pubkey: user2,
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();

    assert_eq!(state.account_type, AccountType::GlobalState);
    assert_eq!(state.foundation_allowlist.len(), 1);
    assert!(!state.foundation_allowlist.contains(&user1));
    assert!(!state.foundation_allowlist.contains(&user2));

    println!("✅ Allowlist is correct");
    /*****************************************************************************************************************************************************/
    println!("🟢🟢🟢  End test_foundation_allowlist  🟢🟢🟢");
}

/// A non-foundation key holding a GLOBALSTATE_ADMIN Permission account can add to
/// the foundation allowlist — exercises the new Permission-account authorization path.
#[tokio::test]
async fn test_foundation_allowlist_add_with_permission_account_allowed() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // A keypair that is NOT in the foundation allowlist.
    let gs_admin = Keypair::new();
    transfer(&mut banks_client, &payer, &gs_admin.pubkey(), 10_000_000).await;

    // Foundation grants it a Permission account with GLOBALSTATE_ADMIN.
    let (permission_pda, _) = get_permission_pda(&program_id, &gs_admin.pubkey());
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: gs_admin.pubkey(),
            permissions: permission_flags::GLOBALSTATE_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // The GLOBALSTATE_ADMIN holder adds a new member to the foundation allowlist,
    // passing its Permission PDA as the optional trailing account authorize() reads.
    let new_member = Pubkey::new_unique();
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    let mut tx = create_transaction_with_extra_accounts(
        program_id,
        &DoubleZeroInstruction::AddFoundationAllowlist(AddFoundationAllowlistArgs {
            pubkey: new_member,
        }),
        &vec![AccountMeta::new(globalstate_pubkey, false)],
        &gs_admin,
        &[AccountMeta::new_readonly(permission_pda, false)],
    );
    tx.try_sign(&[&gs_admin], recent_blockhash).unwrap();
    banks_client
        .process_transaction(tx)
        .await
        .expect("GLOBALSTATE_ADMIN permission holder should add to foundation allowlist");

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();
    assert!(state.foundation_allowlist.contains(&new_member));

    println!("✅ AddFoundationAllowlist with GLOBALSTATE_ADMIN permission succeeded");
}

#[tokio::test]
async fn test_remove_last_foundation_member_rejected() {
    // Isolates the "cannot remove the last member" guard from the self-removal guard: the
    // signer is a GLOBALSTATE_ADMIN who is NOT in the allowlist, so removing the sole
    // member (the init payer) does not trip the self-guard. An empty allowlist would
    // permanently break foundation-only Permission recovery, so it must be rejected.
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // A GLOBALSTATE_ADMIN key outside the allowlist (allowlist is just [payer], len 1).
    let gs_admin = Keypair::new();
    transfer(&mut banks_client, &payer, &gs_admin.pubkey(), 10_000_000).await;
    let (permission_pda, _) = get_permission_pda(&program_id, &gs_admin.pubkey());
    let rb = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        rb,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: gs_admin.pubkey(),
            permissions: permission_flags::GLOBALSTATE_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // gs_admin tries to remove the only member (the init payer) -> rejected.
    let rb = wait_for_new_blockhash(&mut banks_client).await;
    let result = try_execute_transaction_with_extra_accounts(
        &mut banks_client,
        rb,
        program_id,
        DoubleZeroInstruction::RemoveFoundationAllowlist(RemoveFoundationAllowlistArgs {
            pubkey: payer.pubkey(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &gs_admin,
        &[AccountMeta::new_readonly(permission_pda, false)],
    )
    .await;

    assert_custom_error(result, INVALID_FOUNDATION_ALLOWLIST);

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();
    assert_eq!(state.foundation_allowlist.len(), 1);
    assert!(state.foundation_allowlist.contains(&payer.pubkey()));
}

#[tokio::test]
async fn test_remove_self_from_foundation_allowlist_rejected() {
    // Isolates the self-removal guard from the last-member guard: with two members the
    // len>1 check would pass, so only the self-guard can reject the payer removing itself.
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Add a second member so the allowlist has len 2 (the last-member guard won't fire).
    let other = Pubkey::new_unique();
    let rb = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        rb,
        program_id,
        DoubleZeroInstruction::AddFoundationAllowlist(AddFoundationAllowlistArgs { pubkey: other }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // payer (a foundation member) tries to remove itself -> rejected by the self-guard.
    let rb = wait_for_new_blockhash(&mut banks_client).await;
    let result = try_execute_transaction(
        &mut banks_client,
        rb,
        program_id,
        DoubleZeroInstruction::RemoveFoundationAllowlist(RemoveFoundationAllowlistArgs {
            pubkey: payer.pubkey(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    assert_custom_error(result, INVALID_FOUNDATION_ALLOWLIST);

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();
    assert_eq!(state.foundation_allowlist.len(), 2);
    assert!(state.foundation_allowlist.contains(&payer.pubkey()));
    assert!(state.foundation_allowlist.contains(&other));
}
