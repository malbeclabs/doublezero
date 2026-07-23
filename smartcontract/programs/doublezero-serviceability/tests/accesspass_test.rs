use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::{
            check_status::CheckStatusAccessPassArgs, close::CloseAccessPassArgs,
            set::SetAccessPassArgs, AIRDROP_USER_RENT_LAMPORTS_BYTES,
        },
        contributor::create::ContributorCreateArgs,
        device::update::DeviceUpdateArgs,
        globalstate::setauthority::SetAuthorityArgs,
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
    signature::Keypair, signer::Signer,
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
            max_unicast_users: 1,
            max_multicast_users: 1,
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
            max_unicast_users: 1,
            max_multicast_users: 1,
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
            max_unicast_users: 1,
            max_multicast_users: 1,
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
            max_unicast_users: 1,
            max_multicast_users: 1,
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
            AccountMeta::new(solana_system_interface::program::ID, false),
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

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: tenant_acme_code.to_string(),
            administrator: payer.pubkey(),
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

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: tenant_corp_code.to_string(),
            administrator: payer.pubkey(),
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

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: tenant_validator_code.to_string(),
            administrator: payer.pubkey(),
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
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey_1, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer_1, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(tenant_acme, false),
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
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey_2, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer_2, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(tenant_corp, false),
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
            max_unicast_users: 1,
            max_multicast_users: 1,
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
    // When no tenant accounts are passed, the allowlist is empty
    assert_eq!(accesspass_3.tenant_allowlist.len(), 0);
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
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey_1, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer_1, false),
            AccountMeta::new(tenant_acme, false),
            AccountMeta::new(tenant_corp, false),
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
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey_1, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer_1, false),
            AccountMeta::new(tenant_corp, false),
            AccountMeta::new(Pubkey::default(), false),
        ],
        &payer,
    )
    .await;

    let accesspass_1_no_tenant = get_account_data(&mut banks_client, accesspass_pubkey_1)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    // When tenant is removed, the allowlist becomes empty
    assert_eq!(accesspass_1_no_tenant.tenant_allowlist.len(), 0);
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
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey_4, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer_4, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(tenant_validator, false),
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

    // accesspass_1 had its tenant removed in test 5
    assert_eq!(accesspass_check_1.tenant_allowlist.len(), 0);
    // accesspass_2 has tenant_corp
    assert_eq!(accesspass_check_2.tenant_allowlist.len(), 1);
    assert_eq!(accesspass_check_2.tenant_allowlist[0], tenant_corp);
    // accesspass_3 was created without tenant accounts
    assert_eq!(accesspass_check_3.tenant_allowlist.len(), 0);
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
        unicast_user_count: 0,
        max_unicast_users: 1,
        multicast_user_count: 0,
        max_multicast_users: 1,
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
            max_unicast_users: 1,
            max_multicast_users: 1,
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
            max_unicast_users: 1,
            max_multicast_users: 1,
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
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
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

    // Create two tenants (payer is administrator so they can add tenants to access passes)
    let (tenant_a, _) = get_tenant_pda(&program_id, "tenant-a");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: "tenant-a".to_string(),
            administrator: payer.pubkey(),
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
            administrator: payer.pubkey(),
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
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(tenant_a, false),
        ],
        &payer,
    )
    .await;

    // Create user with matching tenant_a → should succeed
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::IBRL);
    let (user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pda, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
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
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(tenant_a, false),
        ],
        &payer,
    )
    .await;

    // Try to create user with tenant_b (wrong tenant) → should fail with error 79
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::IBRL);
    let (user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pda, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
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
async fn test_user_create_with_empty_tenant_allowlist_rejects_tenant() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey, tenant_a, _) =
        setup_device_and_tenants().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let user_ip: Ipv4Addr = [100, 0, 0, 30].into();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user_ip, &payer.pubkey());

    // Set access pass without tenant (no tenant accounts passed)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Create user with tenant_a → should fail because access pass has no tenant allowlist
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::IBRL);
    let (user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pda, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
            AccountMeta::new(tenant_a, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err());
    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(79)"),
        "Expected TenantNotInAccessPassAllowlist error (Custom(79)), got: {}",
        error_string
    );
    println!("✅ User creation with tenant correctly rejected when access-pass has empty tenant allowlist");
}

#[tokio::test]
async fn test_set_accesspass_unauthorized_payer_fails() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    println!("🟢 Start test_set_accesspass_unauthorized_payer_fails");

    // Create an unauthorized payer (not in foundation_allowlist)
    let unauthorized_payer = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &unauthorized_payer.pubkey(),
        10_000_000_000,
    )
    .await;

    let client_ip = Ipv4Addr::new(100, 0, 0, 50);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    // Try to create access pass with unauthorized payer → should fail
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 10,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &unauthorized_payer,
    )
    .await;

    assert!(
        res.is_err(),
        "SetAccessPass should fail when payer is not in foundation_allowlist"
    );

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(8)"),
        "Expected NotAllowed error (Custom(8)), got: {}",
        error_string
    );

    println!("✅ SetAccessPass correctly rejected unauthorized payer (error 8)");
}

#[tokio::test]
async fn test_set_accesspass_with_tenant_admin_succeeds() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    println!("🟢 Start test_set_accesspass_with_tenant_admin_succeeds");

    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    // Create a tenant with a specific administrator
    let tenant_admin = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &tenant_admin.pubkey(),
        10_000_000_000,
    )
    .await;

    let tenant_code = "admin-tenant";
    let (tenant_pubkey, _) = get_tenant_pda(&program_id, tenant_code);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: tenant_code.to_string(),
            administrator: tenant_admin.pubkey(),
            token_account: None,
            metro_routing: true,
            route_liveness: false,
        }),
        vec![
            AccountMeta::new(tenant_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    let client_ip = Ipv4Addr::new(100, 0, 0, 60);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    // Create access pass with tenant using tenant_admin (who is an administrator) → should succeed
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 10,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(tenant_pubkey, false),
        ],
        &tenant_admin,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get AccessPass")
        .get_accesspass()
        .unwrap();

    assert_eq!(accesspass.tenant_allowlist.len(), 1);
    assert_eq!(accesspass.tenant_allowlist[0], tenant_pubkey);

    println!("✅ SetAccessPass with tenant succeeded when payer is tenant administrator");
}

#[tokio::test]
async fn test_set_accesspass_with_tenant_non_admin_fails() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    println!("🟢 Start test_set_accesspass_with_tenant_non_admin_fails");

    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    // Create a tenant with a specific administrator
    let tenant_admin = Pubkey::new_unique();
    let tenant_code = "locked-tenant";
    let (tenant_pubkey, _) = get_tenant_pda(&program_id, tenant_code);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: tenant_code.to_string(),
            administrator: tenant_admin,
            token_account: None,
            metro_routing: true,
            route_liveness: false,
        }),
        vec![
            AccountMeta::new(tenant_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    let client_ip = Ipv4Addr::new(100, 0, 0, 70);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    // Try to create access pass with tenant using payer (who is NOT an administrator) → should fail
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 10,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(tenant_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        res.is_err(),
        "SetAccessPass should fail when payer is not an administrator of the tenant"
    );

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(22)"),
        "Expected Unauthorized error (Custom(22)), got: {}",
        error_string
    );

    println!("✅ SetAccessPass correctly rejected non-administrator payer for tenant (error 22)");
}

#[tokio::test]
async fn test_set_accesspass_tenant_admin_cannot_replace_other_tenant() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    println!("🟢 Start test_set_accesspass_tenant_admin_cannot_replace_other_tenant");

    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    // Tenant A and its administrator.
    let admin_acme = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &admin_acme.pubkey(),
        10_000_000_000,
    )
    .await;
    let acme_code = "acme-tenant";
    let (tenant_acme, _) = get_tenant_pda(&program_id, acme_code);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: acme_code.to_string(),
            administrator: admin_acme.pubkey(),
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

    // Tenant B and its administrator.
    let admin_hyper = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &admin_hyper.pubkey(),
        10_000_000_000,
    )
    .await;
    let hyper_code = "hyper-tenant";
    let (tenant_hyper, _) = get_tenant_pda(&program_id, hyper_code);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: hyper_code.to_string(),
            administrator: admin_hyper.pubkey(),
            token_account: None,
            metro_routing: true,
            route_liveness: false,
        }),
        vec![
            AccountMeta::new(tenant_hyper, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // admin_acme creates an access pass scoped to tenant_acme.
    let client_ip = Ipv4Addr::new(100, 0, 0, 80);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 10,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
            AccountMeta::new(Pubkey::default(), false),
            AccountMeta::new(tenant_acme, false),
        ],
        &admin_acme,
    )
    .await;

    // admin_hyper now attempts to update the same AP, asking the program to swap
    // tenant_acme out for tenant_hyper. This is the SDK "replace" flow. Must fail.
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 20,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
            AccountMeta::new(tenant_acme, false), // tenant_remove (not administered by admin_hyper)
            AccountMeta::new(tenant_hyper, false), // tenant_add
        ],
        &admin_hyper,
    )
    .await;

    assert!(
        res.is_err(),
        "SetAccessPass must reject tenant_remove when payer does not administer the removed tenant"
    );

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(8)"),
        "Expected NotAllowed error (Custom(8)), got: {}",
        error_string
    );

    println!("✅ SetAccessPass correctly rejected cross-tenant replacement");
}

#[tokio::test]
async fn test_set_accesspass_with_sentinel_authority_succeeds() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    println!("🟢 Start test_set_accesspass_with_sentinel_authority_succeeds");

    // Promote a brand-new keypair to sentinel authority.
    let sentinel = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &sentinel.pubkey(),
        10_000_000_000,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            sentinel_authority_pk: Some(sentinel.pubkey()),
            ..Default::default()
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // Sentinel (not in foundation_allowlist) creates an access pass without --tenant.
    let client_ip = Ipv4Addr::new(100, 0, 0, 90);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 100,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &sentinel,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get AccessPass")
        .get_accesspass()
        .unwrap();

    assert_eq!(accesspass.owner, sentinel.pubkey());
    assert!(accesspass.tenant_allowlist.is_empty());

    println!("✅ SetAccessPass succeeded for sentinel authority");
}

#[tokio::test]
async fn test_set_accesspass_with_feed_authority_succeeds() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    println!("🟢 Start test_set_accesspass_with_feed_authority_succeeds");

    // Promote a brand-new keypair to feed authority.
    let feed = Keypair::new();
    transfer(&mut banks_client, &payer, &feed.pubkey(), 10_000_000_000).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            feed_authority_pk: Some(feed.pubkey()),
            ..Default::default()
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // Feed (not in foundation_allowlist) creates an access pass without --tenant.
    let client_ip = Ipv4Addr::new(100, 0, 0, 91);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 100,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &feed,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get AccessPass")
        .get_accesspass()
        .unwrap();

    assert_eq!(accesspass.owner, feed.pubkey());
    assert!(accesspass.tenant_allowlist.is_empty());

    println!("✅ SetAccessPass succeeded for feed authority");
}

#[tokio::test]
async fn test_set_accesspass_tenant_admin_without_tenant_accounts_fails() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    println!("🟢 Start test_set_accesspass_tenant_admin_without_tenant_accounts_fails");

    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    // Tenant exists with admin = tenant_admin keypair.
    let tenant_admin = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &tenant_admin.pubkey(),
        10_000_000_000,
    )
    .await;
    let tenant_code = "scoped-tenant";
    let (tenant_pubkey, _) = get_tenant_pda(&program_id, tenant_code);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: tenant_code.to_string(),
            administrator: tenant_admin.pubkey(),
            token_account: None,
            metro_routing: true,
            route_liveness: false,
        }),
        vec![
            AccountMeta::new(tenant_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // tenant_admin attempts to create an access pass WITHOUT passing the tenant accounts.
    let client_ip = Ipv4Addr::new(100, 0, 0, 92);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 100,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &tenant_admin,
    )
    .await;

    assert!(
        res.is_err(),
        "SetAccessPass must reject a tenant administrator when no tenant accounts are supplied"
    );

    let error_string = format!("{:?}", res.unwrap_err());
    assert!(
        error_string.contains("Custom(8)"),
        "Expected NotAllowed error (Custom(8)), got: {}",
        error_string
    );

    println!("✅ SetAccessPass correctly rejected tenant admin without tenant accounts");
}

/// The SetAccessPass airdrop scales with the per-category caps only when `allow_multiple_ip` is
/// set: a flag-off pass gets the fixed 1x airdrop (regardless of its max fields), while a flag-on
/// pass gets an airdrop scaled by `max_unicast_users + max_multicast_users`.
#[tokio::test]
async fn test_set_accesspass_airdrop_scales_with_allow_multiple_ip() {
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

    // Baseline pass: allow_multiple_ip = false → multiplier is 1 even though caps are > 1.
    let base_user_payer = Pubkey::new_unique();
    let base_ip = Ipv4Addr::new(100, 0, 0, 1);
    let (base_pass, _) = get_accesspass_pda(&program_id, &base_ip, &base_user_payer);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: base_ip,
            last_access_epoch: u64::MAX,
            allow_multiple_ip: false,
            max_unicast_users: 3,
            max_multicast_users: 2,
        }),
        vec![
            AccountMeta::new(base_pass, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(base_user_payer, false),
        ],
        &payer,
    )
    .await;
    let base_balance = banks_client
        .get_account(base_user_payer)
        .await
        .unwrap()
        .expect("base user_payer should be funded")
        .lamports;

    // Scaled pass: allow_multiple_ip = true → multiplier is max_unicast + max_multicast = 5.
    let scaled_user_payer = Pubkey::new_unique();
    let scaled_ip = Ipv4Addr::new(100, 0, 0, 2);
    let (scaled_pass, _) = get_accesspass_pda(&program_id, &scaled_ip, &scaled_user_payer);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: scaled_ip,
            last_access_epoch: u64::MAX,
            allow_multiple_ip: true,
            max_unicast_users: 3,
            max_multicast_users: 2,
        }),
        vec![
            AccountMeta::new(scaled_pass, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(scaled_user_payer, false),
        ],
        &payer,
    )
    .await;
    let scaled_balance = banks_client
        .get_account(scaled_user_payer)
        .await
        .unwrap()
        .expect("scaled user_payer should be funded")
        .lamports;

    assert_eq!(
        scaled_balance,
        base_balance * 5,
        "allow_multiple_ip pass should airdrop (max_unicast + max_multicast)x the base"
    );
}

/// SetAccessPass tops off the pass's `user_payer` so it holds enough SOL to pay rent and
/// connect. The FeedOracle relies on this to refill accounts that already have an AccessPass.
/// This exercises that refill cycle: set an EdgeSeat pass and confirm the funding transfer,
/// drain the `user_payer`, then re-run SetAccessPass with identical params and confirm the
/// balance is restored to the rent + airdrop target.
#[tokio::test]
#[ignore = "EdgeSeat writes hard-disabled pending the 0.30.0 compat floor (EDGE_SEAT_WRITES_DISABLED)"]
async fn test_set_accesspass_refills_depleted_user_payer() {
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

    // The top-off target for a single-user (multiplier 1) pass: rent for the User accounts the
    // payer must fund plus the configured per-user airdrop set by InitGlobalState.
    let user_airdrop_lamports = get_globalstate(&mut banks_client, globalstate_pubkey)
        .await
        .user_airdrop_lamports;
    let target = banks_client
        .get_rent()
        .await
        .unwrap()
        .minimum_balance(AIRDROP_USER_RENT_LAMPORTS_BYTES)
        + user_airdrop_lamports;

    // A real keypair (not Pubkey::new_unique) so the test can sign a transfer to drain it.
    let user_payer = Keypair::new();
    let (accesspass_pubkey, _) =
        get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &user_payer.pubkey());

    let set_access_pass_args = SetAccessPassArgs {
        accesspass_type: AccessPassType::EdgeSeat(vec![]),
        client_ip: Ipv4Addr::UNSPECIFIED,
        last_access_epoch: u64::MAX,
        allow_multiple_ip: false,
        max_unicast_users: 1,
        max_multicast_users: 1,
    };
    let set_access_pass_accounts = vec![
        AccountMeta::new(accesspass_pubkey, false),
        AccountMeta::new(globalstate_pubkey, false),
        AccountMeta::new(user_payer.pubkey(), false),
    ];

    // First SetAccessPass: user_payer starts at zero, so the full target is transferred in.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(set_access_pass_args.clone()),
        set_access_pass_accounts.clone(),
        &payer,
    )
    .await;

    let funded_balance = banks_client
        .get_account(user_payer.pubkey())
        .await
        .unwrap()
        .expect("user_payer should be funded by the first SetAccessPass")
        .lamports;
    assert_eq!(
        funded_balance, target,
        "first SetAccessPass should top user_payer up to the rent + airdrop target"
    );

    // Drain most of the balance so it falls below the target, simulating a spent account. We
    // leave a little behind because the `transfer` helper makes user_payer the fee payer, so it
    // can't move its entire balance (the fee would push it negative).
    transfer(&mut banks_client, &user_payer, &payer.pubkey(), target / 2).await;
    let drained_balance = banks_client
        .get_account(user_payer.pubkey())
        .await
        .unwrap()
        .expect("user_payer should still exist after a partial drain")
        .lamports;
    assert!(
        drained_balance < target,
        "user_payer balance ({drained_balance}) should be below the target ({target}) before the refill"
    );

    // Second SetAccessPass with identical params: the refill restores the target balance.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(set_access_pass_args),
        set_access_pass_accounts,
        &payer,
    )
    .await;

    let refilled_balance = banks_client
        .get_account(user_payer.pubkey())
        .await
        .unwrap()
        .expect("user_payer should be re-funded by the second SetAccessPass")
        .lamports;
    assert_eq!(
        refilled_balance, target,
        "second SetAccessPass should refill the depleted user_payer back to the target"
    );
}
