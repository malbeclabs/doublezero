use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::set::SetAccessPassArgs, contributor::create::ContributorCreateArgs,
        device::update::DeviceUpdateArgs, migrate::MigrateArgs, user::create::*, *,
    },
    resource::ResourceType,
    state::{
        accesspass::{AccessPassStatus, AccessPassType},
        accounttype::AccountType,
        contributor::ContributorStatus,
        device::*,
        user::{UserCYOA, UserStatus, UserType},
    },
};
use globalconfig::set::SetGlobalConfigArgs;
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signer::Signer};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_user_migration() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    /***********************************************************************************************************************************/
    println!("🟢  Start test_user");

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

    let (config_pubkey, _) = get_globalconfig_pda(&program_id);
    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/16".parse().unwrap(),
            next_bgp_community: None,
        }),
        vec![
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(user_tunnel_block_pda, false),
            AccountMeta::new(multicastgroup_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    /***********************************************************************************************************************************/
    println!("🟢 2. Create Location...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 0);

    let (location_pubkey, _) = get_location_pda(&program_id, globalstate_account.account_index + 1);

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

    /***********************************************************************************************************************************/
    println!("🟢 3. Create Exchange...");

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 1);

    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate_account.account_index + 1);

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
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    /***********************************************************************************************************************************/
    println!("🟢 4. Create Contributor...");
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 2);

    let (contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

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

    let contributor = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.account_type, AccountType::Contributor);
    assert_eq!(contributor.code, "cont".to_string());
    assert_eq!(contributor.status, ContributorStatus::Activated);

    println!("✅ Contributor initialized successfully",);
    /***********************************************************************************************************************************/
    // Device _la
    println!("🟢 5. Testing Device initialization...");

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 3);

    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
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

    let device_la = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_la.account_type, AccountType::Device);
    assert_eq!(device_la.code, "la".to_string());
    assert_eq!(device_la.status, DeviceStatus::Pending);

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

    let device_la = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(device_la.max_users, 128);

    println!("✅ Device initialized successfully",);
    /*****************************************************************************************************************************************************/
    println!("🟢 6. Testing Activate Device...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(device::activate::DeviceActivateArgs {
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    let device_la = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_la.account_type, AccountType::Device);
    assert_eq!(device_la.status, DeviceStatus::Activated);

    println!("✅ Device activated successfully");
    /***********************************************************************************************************************************/
    println!("🟢 7. Testing Access Pass creation...");

    let user_ip = [100, 0, 0, 1].into();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user_ip, &payer.pubkey());

    println!("Testing AccessPass User1 initialization...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Check account data
    let user1 = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get User")
        .get_accesspass()
        .unwrap();
    assert_eq!(user1.account_type, AccountType::AccessPass);
    assert_eq!(user1.status, AccessPassStatus::Requested);
    /***********************************************************************************************************************************/
    // Device _la
    println!("🟢 8. Testing User creation...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 4);

    let (user_old_pubkey, _) = get_user_old_pda(&program_id, globalstate_account.account_index + 1);

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
            AccountMeta::new(user_old_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let old_user = get_account_data(&mut banks_client, user_old_pubkey)
        .await
        .expect("Unable to get Account")
        .get_user()
        .unwrap();
    assert_eq!(old_user.account_type, AccountType::User);
    assert_eq!(old_user.client_ip.to_string(), "100.0.0.1");
    assert_eq!(old_user.device_pk, device_pubkey);
    assert_eq!(old_user.status, UserStatus::Pending);

    println!("✅ User created successfully",);
    /***********************************************************************************************************************************/
    // Device _la
    println!("🟢 9. Testing User migration...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 5);

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::IBRL);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::Migrate(MigrateArgs {}),
        vec![
            AccountMeta::new(user_old_pubkey, false),
            AccountMeta::new(user_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get Account")
        .get_user()
        .unwrap();

    assert_eq!(old_user.account_type, user.account_type);
    assert_eq!(old_user.client_ip, user.client_ip);
    assert_eq!(old_user.device_pk, user.device_pk);
    assert_eq!(old_user.status, user.status);
    assert_eq!(old_user.user_type, user.user_type);
    assert_eq!(old_user.cyoa_type, user.cyoa_type);
    assert_eq!(old_user.dz_ip, user.dz_ip);

    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.client_ip.to_string(), "100.0.0.1");
    assert_eq!(user.device_pk, device_pubkey);
    assert_eq!(user.status, UserStatus::Pending);

    println!("✅ User created successfully",);

    println!("🟢🟢🟢  End test_user  🟢🟢🟢");
}
