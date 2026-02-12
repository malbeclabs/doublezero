use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::set::SetAccessPassArgs,
        contributor::create::ContributorCreateArgs,
        device::update::DeviceUpdateArgs,
        tenant::create::TenantCreateArgs,
        user::{activate::*, ban::*, create::*, delete::*, requestban::*, update::*},
        *,
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
use std::net::Ipv4Addr;
use user::closeaccount::UserCloseAccountArgs;

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_user() {
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
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

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
            multicast_publisher_block: "147.51.126.0/23".parse().unwrap(),
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
            AccountMeta::new(multicast_publisher_block_pda, false),
            AccountMeta::new(vrf_ids_pda, false),
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
    println!("🟢 5. Create Contributor...");
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
    println!("🟢 4. Testing Device initialization...");

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
    println!("🟢 5. Testing Activate Device...");
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
    println!("🟢 6. Testing Access Pass creation...");

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
    println!("🟢 7. Testing User creation...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 4);

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::IBRL);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get Account")
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.client_ip.to_string(), "100.0.0.1");
    assert_eq!(user.device_pk, device_pubkey);
    assert_eq!(user.status, UserStatus::Pending);

    println!("✅ User created successfully",);
    /***********************************************************************************************************************************/
    println!("🟢 8. Testing User activation...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0, // legacy path - no ResourceExtension accounts
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get Account")
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.tunnel_id, 500);
    assert_eq!(user.tunnel_net.to_string(), "169.254.0.0/25");
    assert_eq!(user.dz_ip.to_string(), "200.0.0.1");
    assert_eq!(user.status, UserStatus::Activated);

    println!("✅ User created successfully",);
    /*****************************************************************************************************************************************************/
    println!("🟢 9. Testing User update...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
            user_type: Some(UserType::IBRL),
            cyoa_type: Some(UserCYOA::GREOverPrivatePeering),
            dz_ip: Some([200, 0, 0, 4].into()),
            tunnel_id: Some(501),
            tunnel_net: Some("169.254.0.2/25".parse().unwrap()),
            validator_pubkey: None,
            tenant_pk: None,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get Account")
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.client_ip.to_string(), "100.0.0.1");
    assert_eq!(user.cyoa_type, UserCYOA::GREOverPrivatePeering);
    assert_eq!(user.status, UserStatus::Activated);

    println!("✅ User updated");
    /*****************************************************************************************************************************************************/
    println!("🟢 10. Testing User update (regression test: unspecified dz_ip should not clear the dz_ip)...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
            user_type: Some(UserType::IBRL),
            cyoa_type: Some(UserCYOA::GREOverPrivatePeering),
            dz_ip: None,
            tunnel_id: Some(505),
            tunnel_net: Some("169.254.0.2/25".parse().unwrap()),
            validator_pubkey: None,
            tenant_pk: None,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get Account")
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.client_ip.to_string(), "100.0.0.1");
    assert_eq!(user.cyoa_type, UserCYOA::GREOverPrivatePeering);
    assert_eq!(user.status, UserStatus::Activated);
    assert_eq!(user.dz_ip.to_string(), "200.0.0.4");

    println!("✅ User updated");
    /*****************************************************************************************************************************************************/
    println!("🟢 11. Testing User deletion...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {}),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get Account")
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.client_ip.to_string(), "100.0.0.1");
    assert_eq!(user.cyoa_type, UserCYOA::GREOverPrivatePeering);
    assert_eq!(user.status, UserStatus::Deleting);

    println!("✅ Link deleting");

    /*****************************************************************************************************************************************************/
    println!("🟢 12. Testing User deactivation...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {
            dz_prefix_count: 0,
            multicast_publisher_count: 0, // legacy path - no ResourceExtension accounts
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(user.owner, false),
            AccountMeta::new(user.device_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey).await;
    assert_eq!(user, None);

    println!("✅ Link deleted successfully");

    println!("🟢🟢🟢  End test_user  🟢🟢🟢");
}

#[tokio::test]
async fn test_user_ban_requires_pendingban() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // Initialize global state
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

    // Set global config
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
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/24".parse().unwrap(),
            multicast_publisher_block: "147.51.126.0/23".parse().unwrap(),
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
            AccountMeta::new(multicast_publisher_block_pda, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Create minimal device, access pass and user, then activate user
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

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
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

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
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

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

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

    // Allow the device to accept users
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

    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

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

    let user_ip = [100, 0, 0, 1].into();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user_ip, &payer.pubkey());

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

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::IBRL);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0, // legacy path - no ResourceExtension accounts
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);

    // Request ban should move status to PendingBan
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RequestBanUser(UserRequestBanArgs {}),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::PendingBan);

    // BanUser should only succeed when status is PendingBan and move it to Banned
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::BanUser(UserBanArgs {}),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Banned);
}

#[tokio::test]
async fn test_user_create_tenant_allowlist_validation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    println!("🟢  Start test_user_create_tenant_allowlist_validation");

    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    // --- Common infrastructure setup ---

    // Create location
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
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

    // Create exchange
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
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
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create contributor
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
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

    // Create device
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

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
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

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
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    // Create tenant_a
    let tenant_a_code = "tenant-a";
    let (tenant_a_pubkey, _) = get_tenant_pda(&program_id, tenant_a_code);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: tenant_a_code.to_string(),
            administrator: Pubkey::new_unique(),
            token_account: None,
            metro_routing: true,
            route_liveness: false,
        }),
        vec![
            AccountMeta::new(tenant_a_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Create tenant_b
    let tenant_b_code = "tenant-b";
    let (tenant_b_pubkey, _) = get_tenant_pda(&program_id, tenant_b_code);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: tenant_b_code.to_string(),
            administrator: Pubkey::new_unique(),
            token_account: None,
            metro_routing: true,
            route_liveness: false,
        }),
        vec![
            AccountMeta::new(tenant_b_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // ==========================================
    // Scenario 1: Access pass with no tenant, user creation with tenant
    // The access pass has tenant_allowlist = [Pubkey::default()] (empty/no tenant).
    // Creating a user that specifies a tenant should fail.
    // ==========================================
    println!("🟢 Scenario 1: Access pass without tenant, user specifies tenant...");

    let user_ip_1 = [100, 0, 0, 1].into();
    let (accesspass_1_pubkey, _) = get_accesspass_pda(&program_id, &user_ip_1, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user_ip_1,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    let (user_1_pubkey, _) = get_user_pda(&program_id, &user_ip_1, UserType::IBRL);

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip_1,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_1_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(tenant_a_pubkey, false),
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

    println!("✅ Scenario 1 passed: correctly rejected tenant when access pass has no tenant");

    // ==========================================
    // Scenario 2: Access pass with tenant_a, user creation with tenant_b
    // The access pass has tenant_allowlist = [tenant_a_pubkey].
    // Creating a user that specifies tenant_b should fail.
    // ==========================================
    println!("🟢 Scenario 2: Access pass with tenant_a, user specifies tenant_b...");

    let user_ip_2 = [100, 0, 0, 2].into();
    let (accesspass_2_pubkey, _) = get_accesspass_pda(&program_id, &user_ip_2, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user_ip_2,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            tenant: tenant_a_pubkey,
        }),
        vec![
            AccountMeta::new(accesspass_2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    let (user_2_pubkey, _) = get_user_pda(&program_id, &user_ip_2, UserType::IBRL);

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip_2,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_2_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(tenant_b_pubkey, false),
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

    println!("✅ Scenario 2 passed: correctly rejected mismatched tenant");
    println!("🟢🟢🟢  End test_user_create_tenant_allowlist_validation  🟢🟢🟢");
}

#[tokio::test]
async fn test_user_per_type_limits() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    println!("🟢 Testing per-type user limits (unicast/multicast)");

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // Initialize global state
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

    // Set global config
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
    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/24".parse().unwrap(),
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
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    // Create location, exchange, contributor
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
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

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
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

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
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

    // Create device with per-type limits: max_unicast_users=1, max_multicast_users=1
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

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

    // Set max_users, max_unicast_users, and max_multicast_users
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_users: Some(128),
            max_unicast_users: Some(1),
            max_multicast_users: Some(1),
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

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(device.max_unicast_users, 1);
    assert_eq!(device.max_multicast_users, 1);
    assert_eq!(device.unicast_users_count, 0);
    assert_eq!(device.multicast_users_count, 0);

    // Activate device
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

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

    // Create access pass for first unicast user
    let user1_ip = [100, 0, 0, 10].into();
    let (accesspass1_pubkey, _) = get_accesspass_pda(&program_id, &user1_ip, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user1_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Create first unicast user (should succeed)
    println!("🟢 Creating first unicast user (should succeed)");
    let (user1_pubkey, _) = get_user_pda(&program_id, &user1_ip, UserType::IBRL);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user1_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user1_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify counters updated
    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(device.unicast_users_count, 1);
    assert_eq!(device.multicast_users_count, 0);
    assert_eq!(device.users_count, 1);

    println!("✅ First unicast user created, counters updated correctly");

    // Create access pass for second unicast user
    let user2_ip = [100, 0, 0, 11].into();
    let (accesspass2_pubkey, _) = get_accesspass_pda(&program_id, &user2_ip, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user2_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Try to create second unicast user (should fail with MaxUnicastUsersExceeded)
    println!("🟢 Creating second unicast user (should fail)");
    let (user2_pubkey, _) = get_user_pda(&program_id, &user2_ip, UserType::IBRL);

    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user2_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user2_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Error code 72 = MaxUnicastUsersExceeded
    assert!(
        result.is_err(),
        "Second unicast user creation should have failed"
    );
    println!("✅ Second unicast user correctly rejected with MaxUnicastUsersExceeded");

    // Create access pass for first multicast user
    let mcast1_ip = [100, 0, 0, 20].into();
    let (accesspass_mcast1_pubkey, _) =
        get_accesspass_pda(&program_id, &mcast1_ip, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: mcast1_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_mcast1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Create first multicast user (should succeed)
    println!("🟢 Creating first multicast user (should succeed)");
    let (mcast1_pubkey, _) = get_user_pda(&program_id, &mcast1_ip, UserType::Multicast);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: mcast1_ip,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(mcast1_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_mcast1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify counters updated
    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(device.unicast_users_count, 1);
    assert_eq!(device.multicast_users_count, 1);
    assert_eq!(device.users_count, 2);

    println!("✅ First multicast user created, counters updated correctly");

    // Create access pass for second multicast user
    let mcast2_ip = [100, 0, 0, 21].into();
    let (accesspass_mcast2_pubkey, _) =
        get_accesspass_pda(&program_id, &mcast2_ip, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: mcast2_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_mcast2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Try to create second multicast user (should fail with MaxMulticastUsersExceeded)
    println!("🟢 Creating second multicast user (should fail)");
    let (mcast2_pubkey, _) = get_user_pda(&program_id, &mcast2_ip, UserType::Multicast);

    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: mcast2_ip,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(mcast2_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_mcast2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Error code 73 = MaxMulticastUsersExceeded
    assert!(
        result.is_err(),
        "Second multicast user creation should have failed"
    );
    println!("✅ Second multicast user correctly rejected with MaxMulticastUsersExceeded");

    // =========================================================================
    // Test counter decrement: delete user and verify counter decreases
    // =========================================================================
    println!("🟢 Testing counter decrement on user deletion");

    // First, activate the user (required before deletion - sets dz_ip, tunnel_id, etc.)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user1_pubkey, false),
            AccountMeta::new(accesspass1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Delete first unicast user
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {}),
        vec![
            AccountMeta::new(user1_pubkey, false),
            AccountMeta::new(accesspass1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Close the user account (this decrements the counter)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs { dz_prefix_count: 0 }),
        vec![
            AccountMeta::new(user1_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify unicast counter decremented
    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(
        device.unicast_users_count, 0,
        "Unicast counter should be 0 after deletion"
    );
    assert_eq!(
        device.multicast_users_count, 1,
        "Multicast counter should still be 1"
    );
    assert_eq!(device.users_count, 1, "Total users count should be 1");
    println!("✅ Counters decremented correctly after user deletion");

    // Now we should be able to create another unicast user (limit is 1, count is 0)
    println!("🟢 Creating new unicast user after deletion (should succeed)");
    let (user2_pubkey_new, _) = get_user_pda(&program_id, &user2_ip, UserType::IBRL);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user2_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user2_pubkey_new, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(
        device.unicast_users_count, 1,
        "Unicast counter should be 1 after new user"
    );
    println!("✅ New unicast user created successfully after deletion freed up slot");

    // =========================================================================
    // Test zero = unlimited: set limits to 0 and create multiple users
    // =========================================================================
    println!("🟢 Testing zero = unlimited behavior");

    // Set both limits to 0 (unlimited)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_unicast_users: Some(0),
            max_multicast_users: Some(0),
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

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(device.max_unicast_users, 0, "Max unicast should be 0");
    assert_eq!(device.max_multicast_users, 0, "Max multicast should be 0");

    // Create additional unicast user (should succeed because 0 = unlimited)
    let user3_ip = [100, 0, 0, 12].into();
    let (accesspass3_pubkey, _) = get_accesspass_pda(&program_id, &user3_ip, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user3_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass3_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    let (user3_pubkey, _) = get_user_pda(&program_id, &user3_ip, UserType::IBRL);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user3_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user3_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass3_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create additional multicast user (should succeed because 0 = unlimited)
    let mcast3_ip = [100, 0, 0, 22].into();
    let (accesspass_mcast3_pubkey, _) =
        get_accesspass_pda(&program_id, &mcast3_ip, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: mcast3_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(accesspass_mcast3_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    let (mcast3_pubkey, _) = get_user_pda(&program_id, &mcast3_ip, UserType::Multicast);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: mcast3_ip,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(mcast3_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_mcast3_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(
        device.unicast_users_count, 2,
        "Should have 2 unicast users with unlimited"
    );
    assert_eq!(
        device.multicast_users_count, 2,
        "Should have 2 multicast users with unlimited"
    );
    println!("✅ Zero = unlimited behavior verified: multiple users created with limit=0");

    println!("🟢🟢🟢 Per-type user limits test passed 🟢🟢🟢");
}
