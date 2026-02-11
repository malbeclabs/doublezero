use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::{
            check_status::CheckStatusAccessPassArgs, close::CloseAccessPassArgs,
            set::SetAccessPassArgs,
        },
        contributor::create::ContributorCreateArgs,
        device::{activate::DeviceActivateArgs, update::DeviceUpdateArgs},
        tenant::create::TenantCreateArgs,
        user::create::UserCreateArgs,
        *,
    },
    resource::ResourceType,
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accounttype::AccountType,
        device::{DeviceDesiredStatus, DeviceType},
        user::{UserCYOA, UserType},
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
            metro_routing: true,
            route_liveness: false,
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
            metro_routing: false,
            route_liveness: true,
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
            metro_routing: true,
            route_liveness: true,
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

/// Helper: set up a full environment with a device and tenants for user creation tests.
/// Returns (banks_client, payer, program_id, globalstate_pubkey, device_pubkey, tenant_a, tenant_b).
async fn setup_device_and_tenants() -> (BanksClient, Keypair, Pubkey, Pubkey, Pubkey, Pubkey, Pubkey)
{
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    // Create location
    let (location_pubkey, _) = get_location_pda(&program_id, 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(location::create::LocationCreateArgs {
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

    // Create exchange
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, 2);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(exchange::create::ExchangeCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            lat: 1.234,
            lng: 4.567,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create contributor
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, 3);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "cont".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create device
    let (device_pubkey, _) = get_device_pda(&program_id, 4);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
            code: "la".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "100.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Update device max_users
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_users: Some(128),
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Activate device
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    // Create two tenants
    let (tenant_a, _) = get_tenant_pda(&program_id, "tenant-a");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: "tenant-a".to_string(),
            administrator: Pubkey::new_unique(),
            token_account: None,
            metro_routing: true,
            route_liveness: false,
        }),
        vec![
            AccountMeta::new(tenant_a, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    let (tenant_b, _) = get_tenant_pda(&program_id, "tenant-b");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: "tenant-b".to_string(),
            administrator: Pubkey::new_unique(),
            token_account: None,
            metro_routing: false,
            route_liveness: true,
        }),
        vec![
            AccountMeta::new(tenant_b, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    (
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        tenant_a,
        tenant_b,
    )
}

#[tokio::test]
async fn test_user_create_with_matching_tenant_in_allowlist() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey, tenant_a, _) =
        setup_device_and_tenants().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_ip: Ipv4Addr = [100, 0, 0, 10].into();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user_ip, &payer.pubkey());

    // Set access pass with tenant_a in allowlist
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            tenant: tenant_a,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Create user with matching tenant_a → should succeed
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::IBRL);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(tenant_a, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.tenant_pk, tenant_a);
    println!("✅ User created with matching tenant in allowlist");
}

#[tokio::test]
async fn test_user_create_with_wrong_tenant_in_allowlist() {
    let (
        mut banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
        tenant_a,
        tenant_b,
    ) = setup_device_and_tenants().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_ip: Ipv4Addr = [100, 0, 0, 20].into();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user_ip, &payer.pubkey());

    // Set access pass with tenant_a in allowlist
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            tenant: tenant_a,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Try to create user with tenant_b (wrong tenant) → should fail with error 79
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::IBRL);
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(tenant_b, false),
        ],
        &payer,
    )
    .await;

    assert!(
        res.is_err(),
        "CreateUser should fail when tenant is not in access-pass allowlist"
    );
    println!("✅ User creation rejected with wrong tenant (error 79)");
}

#[tokio::test]
async fn test_user_create_with_default_tenant_allowlist_allows_any() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey, tenant_a, _) =
        setup_device_and_tenants().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_ip: Ipv4Addr = [100, 0, 0, 30].into();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user_ip, &payer.pubkey());

    // Set access pass with default tenant (no restriction)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Create user with tenant_a → should succeed even though access pass has default tenant
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::IBRL);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(tenant_a, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.tenant_pk, tenant_a);
    println!("✅ User created with any tenant when access-pass has default tenant allowlist");
}
