use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        location::{create::*, delete::*, resume::*, suspend::*, update::*},
        permission::create::PermissionCreateArgs,
    },
    state::{accounttype::AccountType, location::*, permission::permission_flags},
};
use solana_program_test::*;
use solana_sdk::{
    instruction::AccountMeta,
    signature::{Keypair, Signer},
};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_location() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    /***********************************************************************************************************************************/
    println!("🟢  Start test_location");

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

    /***********************************************************************************************************************************/
    // Location _la

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    println!("Testing Location initialization...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 0);

    let (location_pubkey, _) = get_location_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
            code: "LA".to_string(),
            name: "Los Angeles".to_string(),
            country: "us".to_string(),
            lat: 1.234,
            lng: 4.567,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let location_la = get_account_data(&mut banks_client, location_pubkey)
        .await
        .expect("Unable to get Account")
        .get_location()
        .unwrap();
    assert_eq!(location_la.account_type, AccountType::Location);
    assert_eq!(location_la.code, "LA".to_string());
    assert_eq!(location_la.status, LocationStatus::Activated);

    println!("✅ Location initialized successfully",);
    /*****************************************************************************************************************************************************/
    println!("Testing Location suspend...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SuspendLocation(LocationSuspendArgs {}),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let location_la = get_account_data(&mut banks_client, location_pubkey)
        .await
        .expect("Unable to get Account")
        .get_location()
        .unwrap();
    assert_eq!(location_la.account_type, AccountType::Location);
    assert_eq!(location_la.status, LocationStatus::Suspended);

    println!("✅ Location suspended");
    /*****************************************************************************************************************************************************/
    println!("Testing Location resumed...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ResumeLocation(LocationResumeArgs {}),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let location = get_account_data(&mut banks_client, location_pubkey)
        .await
        .expect("Unable to get Account")
        .get_location()
        .unwrap();
    assert_eq!(location.account_type, AccountType::Location);
    assert_eq!(location.status, LocationStatus::Activated);

    println!("✅ Location resumed");
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ResumeLocation(LocationResumeArgs {}),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err());
    let error = result.unwrap_err();
    let error_string = format!("{:?}", error);
    assert!(
        error_string.contains("Custom(7)"),
        "Expected error to contain 'Custom(7)' (InvalidStatus), but got: {}",
        error_string
    );
    /*****************************************************************************************************************************************************/
    println!("Testing Location update...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateLocation(LocationUpdateArgs {
            code: Some("LA2".to_string()),
            name: Some("Los Angeles - Los Angeles".to_string()),
            country: Some("CA".to_string()),
            lat: Some(3.433),
            lng: Some(23.223),
            loc_id: Some(1),
        }),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let location_la = get_account_data(&mut banks_client, location_pubkey)
        .await
        .expect("Unable to get Account")
        .get_location()
        .unwrap();
    assert_eq!(location_la.account_type, AccountType::Location);
    assert_eq!(location_la.code, "LA2".to_string());
    assert_eq!(location_la.name, "Los Angeles - Los Angeles".to_string());
    assert_eq!(location_la.status, LocationStatus::Activated);

    println!("✅ Location updated");
    /*****************************************************************************************************************************************************/
    println!("Testing Location deletion...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLocation(LocationDeleteArgs {}),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let location_la = get_account_data(&mut banks_client, location_pubkey).await;
    assert_eq!(location_la, None);

    println!("✅ Location deleted successfully");
    println!("🟢  End test_location");
}

#[tokio::test]
async fn test_location_delete_from_suspended() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // Init global state
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

    // Create location
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            country: "us".to_string(),
            lat: 1.234,
            lng: 4.567,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Suspend and then delete directly from Suspended
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SuspendLocation(LocationSuspendArgs {}),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let location_la = get_account_data(&mut banks_client, location_pubkey)
        .await
        .expect("Unable to get Account")
        .get_location()
        .unwrap();
    assert_eq!(location_la.status, LocationStatus::Suspended);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteLocation(LocationDeleteArgs {}),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let location_la = get_account_data(&mut banks_client, location_pubkey).await;
    assert_eq!(location_la, None);
}

/// A non-foundation key holding an INFRA_ADMIN Permission account can create a
/// location — exercises the new Permission-account authorization path.
#[tokio::test]
async fn test_location_create_with_permission_account_allowed() {
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

    // A key that is NOT in the foundation allowlist, granted INFRA_ADMIN.
    let infra_admin = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &infra_admin.pubkey(),
        1_000_000_000,
    )
    .await;

    let (permission_pda, _) = get_permission_pda(&program_id, &infra_admin.pubkey());
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: infra_admin.pubkey(),
            permissions: permission_flags::INFRA_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // infra_admin creates a location, passing its Permission PDA as the optional
    // trailing account that authorize() reads.
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate.account_index + 1);
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    let mut tx = create_transaction_with_extra_accounts(
        program_id,
        &DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
            code: "permloc".to_string(),
            name: "Permissioned Location".to_string(),
            country: "us".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 0,
        }),
        &vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &infra_admin,
        &[AccountMeta::new_readonly(permission_pda, false)],
    );
    tx.try_sign(&[&infra_admin], recent_blockhash).unwrap();
    banks_client
        .process_transaction(tx)
        .await
        .expect("INFRA_ADMIN permission holder should be able to create a location");

    let location = get_account_data(&mut banks_client, location_pubkey)
        .await
        .expect("location")
        .get_location()
        .unwrap();
    assert_eq!(location.account_type, AccountType::Location);
    assert_eq!(location.code, "permloc".to_string());

    println!("✅ CreateLocation with INFRA_ADMIN permission succeeded");
}
