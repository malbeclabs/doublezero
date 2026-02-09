use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::{
            check_status::CheckStatusAccessPassArgs, close::CloseAccessPassArgs,
            set::SetAccessPassArgs,
        },
        tenant::create::TenantCreateArgs,
    },
    resource::ResourceType,
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accounttype::AccountType,
    },
};
use solana_program::rent::Rent;
use solana_program_test::*;
use solana_sdk::{
    account::Account as SolanaAccount, instruction::AccountMeta, pubkey::Pubkey,
    signature::Keypair, signer::Signer, system_program,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_accesspass() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    /***********************************************************************************************************************************/
    println!("🟢  Start test_accesspass");

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
    // AccessPass tests

    let client_ip = Ipv4Addr::new(100, 0, 0, 1);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);
    let solana_identity = Pubkey::new_unique();

    /***********************************************************************************************************************************/
    println!("🟢 1. Create AccessPass...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 10,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(accesspass.accesspass_type, AccessPassType::Prepaid);
    assert_eq!(accesspass.client_ip, client_ip);
    assert_eq!(accesspass.last_access_epoch, 10);
    println!("✅ AccessPass created successfully");

    /***********************************************************************************************************************************/
    println!("🟢 2. Update AccessPass...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::SolanaValidator(solana_identity),
            client_ip,
            last_access_epoch: u64::MAX,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(
        accesspass.accesspass_type,
        AccessPassType::SolanaValidator(solana_identity)
    );
    assert_eq!(accesspass.client_ip, client_ip);
    assert_eq!(accesspass.last_access_epoch, u64::MAX);
    println!("✅ AccessPass updated successfully");

    /***********************************************************************************************************************************/
    println!("🟢 3. Close AccessPass...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccessPass(CloseAccessPassArgs {}),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let accesspass_closed = get_account_data(&mut banks_client, accesspass_pubkey).await;
    assert!(accesspass_closed.is_none());

    println!("✅ AccessPass closed successfully");

    /***********************************************************************************************************************************/
    println!("🟢 4. Create AccessPass again...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 101,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();

    assert_eq!(accesspass.accesspass_type, AccessPassType::Prepaid);
    assert_eq!(accesspass.client_ip, client_ip);
    assert_eq!(accesspass.last_access_epoch, 101);
    println!("✅ AccessPass recreated successfully");

    /***********************************************************************************************************************************/
    println!("🟢 5. Update AccessPass last_epoch = 0...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 0,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();

    assert_eq!(accesspass.accesspass_type, AccessPassType::Prepaid);
    assert_eq!(accesspass.client_ip, client_ip);
    assert_eq!(accesspass.last_access_epoch, 0);
    println!("✅ AccessPass update last_epoch successfully");

    /***********************************************************************************************************************************/
    println!("🟢 6. Check AccessPass...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CheckStatusAccessPass(CheckStatusAccessPassArgs {}),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();

    assert_eq!(accesspass.accesspass_type, AccessPassType::Prepaid);
    assert_eq!(accesspass.client_ip, client_ip);
    assert_eq!(accesspass.last_access_epoch, 0);
    println!("✅ AccessPass check Access Pass successfully");
    /***********************************************************************************************************************************/
    println!("🟢 6. Check AccessPass (no payer)...");

    let another_payer = Keypair::new();

    transfer(
        &mut banks_client,
        &payer,
        &another_payer.pubkey(),
        1_000_000_000,
    )
    .await;

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CheckStatusAccessPass(CheckStatusAccessPassArgs {}),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(system_program::id(), false),
        ],
        &another_payer,
    )
    .await;

    println!("res: {:?}", res);

    assert!(res.is_err());

    println!("✅ AccessPass check Access Pass fail successfully");
    /***********************************************************************************************************************************/

    println!("🟢  End test_accesspass");
}

#[tokio::test]
async fn test_accesspass_with_tenant() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    /***********************************************************************************************************************************/
    println!("🟢  Start test_accesspass_with_tenant");

    /***********************************************************************************************************************************/
    // Create tenants for testing
    println!("🟢 1.1. Creating tenants...");

    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    let tenant_acme_code = "acme";
    let (tenant_acme, _) = get_tenant_pda(&program_id, tenant_acme_code);
    let administrator_acme = Pubkey::new_unique();

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: tenant_acme_code.to_string(),
            administrator: administrator_acme,
            token_account: None,
        }),
        vec![
            AccountMeta::new(tenant_acme, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    let tenant_corp_code = "corp";
    let (tenant_corp, _) = get_tenant_pda(&program_id, tenant_corp_code);
    let administrator_corp = Pubkey::new_unique();

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: tenant_corp_code.to_string(),
            administrator: administrator_corp,
            token_account: None,
        }),
        vec![
            AccountMeta::new(tenant_corp, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    let tenant_validator_code = "validator-tenant";
    let (tenant_validator, _) = get_tenant_pda(&program_id, tenant_validator_code);
    let administrator_validator = Pubkey::new_unique();

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: tenant_validator_code.to_string(),
            administrator: administrator_validator,
            token_account: None,
        }),
        vec![
            AccountMeta::new(tenant_validator, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    println!("✅ Tenants created successfully");

    /***********************************************************************************************************************************/
    // Test 1: Create AccessPass with tenant
    println!("🟢 2. Create AccessPass with tenant...");

    let client_ip_1 = Ipv4Addr::new(100, 0, 0, 5);
    let user_payer_1 = Pubkey::new_unique();
    let (accesspass_pubkey_1, _) = get_accesspass_pda(&program_id, &client_ip_1, &user_payer_1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: client_ip_1,
            last_access_epoch: 10,
            allow_multiple_ip: false,
            tenant: tenant_acme,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey_1, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer_1, false),
        ],
        &payer,
    )
    .await;

    let accesspass_1 = get_account_data(&mut banks_client, accesspass_pubkey_1)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(accesspass_1.accesspass_type, AccessPassType::Prepaid);
    assert_eq!(accesspass_1.client_ip, client_ip_1);
    assert_eq!(accesspass_1.tenant_allowlist.len(), 1);
    assert_eq!(accesspass_1.tenant_allowlist[0], tenant_acme);
    println!("✅ AccessPass with tenant 'acme' created successfully");

    /***********************************************************************************************************************************/
    // Test 2: Create AccessPass with different tenant
    println!("🟢 3. Create AccessPass with different tenant...");

    let client_ip_2 = Ipv4Addr::new(100, 0, 0, 6);
    let user_payer_2 = Pubkey::new_unique();
    let (accesspass_pubkey_2, _) = get_accesspass_pda(&program_id, &client_ip_2, &user_payer_2);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: client_ip_2,
            last_access_epoch: 20,
            allow_multiple_ip: false,
            tenant: tenant_corp,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey_2, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer_2, false),
        ],
        &payer,
    )
    .await;

    let accesspass_2 = get_account_data(&mut banks_client, accesspass_pubkey_2)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(accesspass_2.accesspass_type, AccessPassType::Prepaid);
    assert_eq!(accesspass_2.client_ip, client_ip_2);
    assert_eq!(accesspass_2.tenant_allowlist.len(), 1);
    assert_eq!(accesspass_2.tenant_allowlist[0], tenant_corp);
    println!("✅ AccessPass with tenant 'corp' created successfully");

    /***********************************************************************************************************************************/
    // Test 3: Create AccessPass without tenant (backward compatibility)
    println!("🟢 4. Create AccessPass without tenant (backward compatibility)...");

    let client_ip_3 = Ipv4Addr::new(10, 10, 10, 10);
    let user_payer_3 = Pubkey::new_unique();
    let (accesspass_pubkey_3, _) = get_accesspass_pda(&program_id, &client_ip_3, &user_payer_3);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: client_ip_3,
            last_access_epoch: 30,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey_3, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer_3, false),
        ],
        &payer,
    )
    .await;

    let accesspass_3 = get_account_data(&mut banks_client, accesspass_pubkey_3)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(accesspass_3.accesspass_type, AccessPassType::Prepaid);
    assert_eq!(accesspass_3.client_ip, client_ip_3);
    // When tenant is Pubkey::default(), it's added to the allowlist
    assert_eq!(accesspass_3.tenant_allowlist.len(), 1);
    assert_eq!(accesspass_3.tenant_allowlist[0], Pubkey::default());
    println!("✅ AccessPass without tenant created successfully (backward compatibility)");

    /***********************************************************************************************************************************/
    // Test 4: Update AccessPass to change tenant
    println!("🟢 5. Update AccessPass to change tenant...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: client_ip_1,
            last_access_epoch: 15,
            allow_multiple_ip: false,
            tenant: tenant_corp,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey_1, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer_1, false),
        ],
        &payer,
    )
    .await;

    let accesspass_1_updated = get_account_data(&mut banks_client, accesspass_pubkey_1)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(accesspass_1_updated.tenant_allowlist.len(), 1);
    assert_eq!(accesspass_1_updated.tenant_allowlist[0], tenant_corp);
    assert_eq!(accesspass_1_updated.last_access_epoch, 15);
    println!("✅ AccessPass tenant updated successfully");

    /***********************************************************************************************************************************/
    // Test 5: Update AccessPass to remove tenant
    println!("🟢 6. Update AccessPass to remove tenant...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: client_ip_1,
            last_access_epoch: 25,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey_1, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer_1, false),
        ],
        &payer,
    )
    .await;

    let accesspass_1_no_tenant = get_account_data(&mut banks_client, accesspass_pubkey_1)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    // When tenant is set to Pubkey::default(), it's still added to the allowlist
    assert_eq!(accesspass_1_no_tenant.tenant_allowlist.len(), 1);
    assert_eq!(
        accesspass_1_no_tenant.tenant_allowlist[0],
        Pubkey::default()
    );
    assert_eq!(accesspass_1_no_tenant.last_access_epoch, 25);
    println!("✅ AccessPass tenant removed successfully");

    /***********************************************************************************************************************************/
    // Test 6: Create AccessPass with SolanaValidator type and tenant
    println!("🟢 7. Create AccessPass with SolanaValidator type and tenant...");

    let client_ip_4 = Ipv4Addr::new(200, 200, 200, 200);
    let user_payer_4 = Pubkey::new_unique();
    let solana_identity = Pubkey::new_unique();
    let (accesspass_pubkey_4, _) = get_accesspass_pda(&program_id, &client_ip_4, &user_payer_4);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::SolanaValidator(solana_identity),
            client_ip: client_ip_4,
            last_access_epoch: u64::MAX,
            allow_multiple_ip: false,
            tenant: tenant_validator,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey_4, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer_4, false),
        ],
        &payer,
    )
    .await;

    let accesspass_4 = get_account_data(&mut banks_client, accesspass_pubkey_4)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(
        accesspass_4.accesspass_type,
        AccessPassType::SolanaValidator(solana_identity)
    );
    assert_eq!(accesspass_4.tenant_allowlist.len(), 1);
    assert_eq!(accesspass_4.tenant_allowlist[0], tenant_validator);
    println!("✅ AccessPass with SolanaValidator type and tenant created successfully");

    /***********************************************************************************************************************************/
    // Test 7: Verify multiple access passes with different tenants coexist
    println!("🟢 8. Verify multiple access passes with different tenants coexist...");

    let accesspass_check_1 = get_account_data(&mut banks_client, accesspass_pubkey_1)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    let accesspass_check_2 = get_account_data(&mut banks_client, accesspass_pubkey_2)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    let accesspass_check_3 = get_account_data(&mut banks_client, accesspass_pubkey_3)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();

    // accesspass_1 was updated to Pubkey::default() in test 5
    assert_eq!(accesspass_check_1.tenant_allowlist.len(), 1);
    assert_eq!(accesspass_check_1.tenant_allowlist[0], Pubkey::default());
    // accesspass_2 has tenant_corp
    assert_eq!(accesspass_check_2.tenant_allowlist.len(), 1);
    assert_eq!(accesspass_check_2.tenant_allowlist[0], tenant_corp);
    // accesspass_3 has Pubkey::default()
    assert_eq!(accesspass_check_3.tenant_allowlist.len(), 1);
    assert_eq!(accesspass_check_3.tenant_allowlist[0], Pubkey::default());
    println!("✅ Multiple access passes with different tenant configurations verified");

    println!("🟢  End test_accesspass_with_tenant");
}

#[tokio::test]
async fn test_close_accesspass_rejects_nonzero_connection_count() {
    // Set up a dedicated ProgramTest so we can pre-seed an AccessPass account
    let program_id = Pubkey::new_unique();

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    let client_ip = Ipv4Addr::new(101, 0, 0, 1);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, bump_seed) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    // Build an AccessPass with connection_count > 0
    let seeded_accesspass = AccessPass {
        account_type: AccountType::AccessPass,
        owner: program_id,
        bump_seed,
        accesspass_type: AccessPassType::Prepaid,
        client_ip,
        user_payer,
        last_access_epoch: 0,
        connection_count: 1,
        status: AccessPassStatus::Connected,
        mgroup_pub_allowlist: vec![],
        mgroup_sub_allowlist: vec![],
        flags: 0,
        tenant_allowlist: vec![],
    };

    let accesspass_data = borsh::to_vec(&seeded_accesspass).unwrap();
    let rent = Rent::default();
    let lamports = rent.minimum_balance(accesspass_data.len());

    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );

    // Pre-seed the AccessPass account owned by the program
    program_test.add_account(
        accesspass_pubkey,
        SolanaAccount {
            lamports,
            data: accesspass_data,
            owner: program_id,
            executable: false,
            rent_epoch: 0,
        },
    );

    let (mut banks_client, payer, recent_blockhash) = program_test.start().await;

    // Initialize global state so that payer is in the foundation_allowlist
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

    // Attempt to close the seeded AccessPass; this should fail because connection_count != 0
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccessPass(CloseAccessPassArgs {}),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        res.is_err(),
        "CloseAccessPass should fail when connection_count > 0"
    );

    // The AccessPass account should still exist after the failed close attempt
    let account_after = banks_client.get_account(accesspass_pubkey).await.unwrap();
    assert!(account_after.is_some());
}

#[tokio::test]
async fn test_tx_lamports_to_pda_before_creation() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    /***********************************************************************************************************************************/
    println!("🟢  Start test_accesspass");

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
    // AccessPass tests

    let client_ip = Ipv4Addr::new(100, 0, 0, 1);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    // Transfer lamports directly to the accesspass_pubkey
    test_helpers::transfer(&mut banks_client, &payer, &accesspass_pubkey, 128 * 6960).await;

    /***********************************************************************************************************************************/
    println!("🟢 1. Create AccessPass...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 10,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(accesspass.accesspass_type, AccessPassType::Prepaid);
    assert_eq!(accesspass.client_ip, client_ip);
    assert_eq!(accesspass.last_access_epoch, 10);
    println!("✅ AccessPass created successfully");

    // Re-execute the same txn
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 10,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;
}
