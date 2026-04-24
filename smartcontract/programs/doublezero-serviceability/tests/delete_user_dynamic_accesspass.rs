use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::set::SetAccessPassArgs,
        contributor::create::ContributorCreateArgs,
        device::update::DeviceUpdateArgs,
        user::{activate::*, create::*, delete::*},
        *,
    },
    resource::ResourceType,
    state::{
        accesspass::{AccessPassStatus, AccessPassType},
        accounttype::AccountType,
        device::*,
        user::{UserCYOA, UserStatus, UserType},
    },
};
use globalconfig::set::SetGlobalConfigArgs;
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Keypair, signer::Signer};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

struct TestEnv {
    banks_client: BanksClient,
    payer: Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    device_pubkey: Pubkey,
}

/// Initialize the program environment up to and including an activated device.
async fn setup_test_env() -> TestEnv {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
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
    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

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

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "9.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/16".parse().unwrap(),
            multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
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
            AccountMeta::new(admin_group_bits_pda, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_facility_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFacility(facility::create::FacilityCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            country: "us".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_metro_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMetro(metro::create::MetroCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            lat: 1.0,
            lng: 2.0,
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

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, gs.account_index + 1);
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

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);
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
            resource_count: 0,
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

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_users: Some(128),
            resource_count: 2,
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

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

    TestEnv {
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
    }
}

/// Create an access pass, create a user, and activate it. Returns (accesspass_pubkey, user_pubkey).
async fn create_and_activate_user(
    env: &mut TestEnv,
    user_ip: Ipv4Addr,
    user_type: UserType,
    accesspass_ip: Ipv4Addr,
    allow_multiple_ip: bool,
) -> (Pubkey, Pubkey) {
    let recent_blockhash = env
        .banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get latest blockhash");

    let (accesspass_pubkey, _) =
        get_accesspass_pda(&env.program_id, &accesspass_ip, &env.payer.pubkey());

    execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: accesspass_ip,
            last_access_epoch: 9999,
            allow_multiple_ip,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
            AccountMeta::new(env.payer.pubkey(), false),
        ],
        &env.payer,
    )
    .await;

    let (user_pubkey, _) = get_user_pda(&env.program_id, &user_ip, user_type);

    execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(env.device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
        ],
        &env.payer,
    )
    .await;

    execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
        ],
        &env.payer,
    )
    .await;

    (accesspass_pubkey, user_pubkey)
}

/// IS_DYNAMIC pass (UNSPECIFIED PDA, no ALLOW_MULTIPLE_IP): DeleteUser succeeds and access pass
/// connection_count decrements to 0. The pass client_ip is NOT reset since allow_multiple_ip=false.
#[tokio::test]
async fn test_delete_user_is_dynamic_pass() {
    let mut env = setup_test_env().await;
    let user_ip: Ipv4Addr = [100, 0, 0, 1].into();

    // IS_DYNAMIC pass: client_ip=UNSPECIFIED, allow_multiple_ip=false
    let (accesspass_pubkey, user_pubkey) = create_and_activate_user(
        &mut env,
        user_ip,
        UserType::IBRL,
        Ipv4Addr::UNSPECIFIED,
        false,
    )
    .await;

    // CreateUser with IS_DYNAMIC pass locks the pass's client_ip to the user's IP
    let pass = get_account_data(&mut env.banks_client, accesspass_pubkey)
        .await
        .unwrap()
        .get_accesspass()
        .unwrap();
    assert_eq!(pass.client_ip, user_ip);
    assert_eq!(pass.connection_count, 1);
    assert_eq!(pass.status, AccessPassStatus::Connected);

    let recent_blockhash = env
        .banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get latest blockhash");

    execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: 0,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
        ],
        &env.payer,
    )
    .await;

    let user = get_account_data(&mut env.banks_client, user_pubkey)
        .await
        .unwrap()
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Deleting);

    let pass = get_account_data(&mut env.banks_client, accesspass_pubkey)
        .await
        .unwrap()
        .get_accesspass()
        .unwrap();
    assert_eq!(pass.connection_count, 0);
    assert_eq!(pass.status, AccessPassStatus::Disconnected);
    // Not allow_multiple_ip — client_ip is NOT reset to UNSPECIFIED
    assert_eq!(pass.client_ip, user_ip);
}

/// ALLOW_MULTIPLE_IP pass: after the last user is deleted, the pass's client_ip resets to
/// UNSPECIFIED so the pass can be reused for any IP.
#[tokio::test]
async fn test_delete_user_allow_multiple_ip_resets_client_ip() {
    let mut env = setup_test_env().await;
    let user_ip: Ipv4Addr = [100, 0, 0, 2].into();

    // ALLOW_MULTIPLE_IP pass at UNSPECIFIED PDA
    let (accesspass_pubkey, user_pubkey) = create_and_activate_user(
        &mut env,
        user_ip,
        UserType::IBRL,
        Ipv4Addr::UNSPECIFIED,
        true,
    )
    .await;

    // Verify pass IP was locked to user_ip during CreateUser
    let pass = get_account_data(&mut env.banks_client, accesspass_pubkey)
        .await
        .unwrap()
        .get_accesspass()
        .unwrap();
    assert_eq!(pass.client_ip, user_ip);
    assert_eq!(pass.connection_count, 1);

    let recent_blockhash = env
        .banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get latest blockhash");

    execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: 0,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
        ],
        &env.payer,
    )
    .await;

    let user = get_account_data(&mut env.banks_client, user_pubkey)
        .await
        .unwrap()
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Deleting);

    let pass = get_account_data(&mut env.banks_client, accesspass_pubkey)
        .await
        .unwrap()
        .get_accesspass()
        .unwrap();
    assert_eq!(pass.connection_count, 0);
    assert_eq!(pass.status, AccessPassStatus::Disconnected);
    // allow_multiple_ip: client_ip resets to UNSPECIFIED when connection_count reaches 0
    assert_eq!(pass.client_ip, Ipv4Addr::UNSPECIFIED);
}

/// Specific-IP pass (not UNSPECIFIED): DeleteUser succeeds when pass.client_ip matches user.client_ip.
#[tokio::test]
async fn test_delete_user_specific_ip_pass() {
    let mut env = setup_test_env().await;
    let user_ip: Ipv4Addr = [100, 0, 0, 3].into();

    // Specific-IP pass at the user's IP PDA
    let (accesspass_pubkey, user_pubkey) =
        create_and_activate_user(&mut env, user_ip, UserType::IBRL, user_ip, false).await;

    let recent_blockhash = env
        .banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get latest blockhash");

    execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: 0,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
        ],
        &env.payer,
    )
    .await;

    let user = get_account_data(&mut env.banks_client, user_pubkey)
        .await
        .unwrap()
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Deleting);

    let pass = get_account_data(&mut env.banks_client, accesspass_pubkey)
        .await
        .unwrap()
        .get_accesspass()
        .unwrap();
    assert_eq!(pass.connection_count, 0);
    assert_eq!(pass.status, AccessPassStatus::Disconnected);
    assert_eq!(pass.client_ip, user_ip);
}

/// Multicast user with IS_DYNAMIC pass: DeleteUser succeeds with the same IP validation logic.
#[tokio::test]
async fn test_delete_multicast_user_dynamic_pass() {
    let mut env = setup_test_env().await;
    let user_ip: Ipv4Addr = [100, 0, 0, 4].into();

    // IS_DYNAMIC pass at UNSPECIFIED PDA, no ALLOW_MULTIPLE_IP
    let (accesspass_pubkey, user_pubkey) = create_and_activate_user(
        &mut env,
        user_ip,
        UserType::Multicast,
        Ipv4Addr::UNSPECIFIED,
        false,
    )
    .await;

    let user = get_account_data(&mut env.banks_client, user_pubkey)
        .await
        .unwrap()
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.user_type, UserType::Multicast);
    assert_eq!(user.status, UserStatus::Activated);

    let recent_blockhash = env
        .banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get latest blockhash");

    execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: 0,
            multicast_publisher_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
        ],
        &env.payer,
    )
    .await;

    let user = get_account_data(&mut env.banks_client, user_pubkey)
        .await
        .unwrap()
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Deleting);

    let pass = get_account_data(&mut env.banks_client, accesspass_pubkey)
        .await
        .unwrap()
        .get_accesspass()
        .unwrap();
    assert_eq!(pass.connection_count, 0);
    assert_eq!(pass.status, AccessPassStatus::Disconnected);
}
