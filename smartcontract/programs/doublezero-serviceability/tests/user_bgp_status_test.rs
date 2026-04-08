use doublezero_serviceability::{
    entrypoint::process_instruction,
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_contributor_pda, get_device_pda, get_exchange_pda,
        get_globalconfig_pda, get_globalstate_pda, get_location_pda, get_program_config_pda,
        get_resource_extension_pda, get_user_pda,
    },
    processors::{
        accesspass::set::SetAccessPassArgs,
        contributor::create::ContributorCreateArgs,
        device::{
            activate::DeviceActivateArgs, create::DeviceCreateArgs, update::DeviceUpdateArgs,
        },
        exchange::create::ExchangeCreateArgs,
        globalconfig::set::SetGlobalConfigArgs,
        location::create::LocationCreateArgs,
        user::{create::UserCreateArgs, set_bgp_status::SetUserBGPStatusArgs},
    },
    resource::ResourceType,
    state::{
        accesspass::AccessPassType,
        device::DeviceType,
        user::{BGPStatus, UserCYOA, UserType},
    },
};
use solana_program::instruction::InstructionError;
use solana_program_test::*;
use solana_sdk::{
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

// ============================================================================
// Setup helper
// ============================================================================

struct BgpStatusTestEnv {
    banks_client: BanksClient,
    /// The keypair used as transaction fee-payer throughout setup; its pubkey is
    /// registered as the device's metrics_publisher_pk.
    payer: Keypair,
    program_id: Pubkey,
    device_pubkey: Pubkey,
    user_pubkey: Pubkey,
}

/// Sets up a minimal environment with an activated device and a created (Pending) user.
///
/// The device is created with `device.metrics_publisher_pk = payer.pubkey()`, so the
/// returned `payer` is the authorized signer for SetUserBGPStatus calls.
async fn setup() -> BgpStatusTestEnv {
    let program_id = Pubkey::new_unique();

    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    let metrics_publisher_pk = payer.pubkey();

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);
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
            device_tunnel_block: "10.100.0.0/24".parse().unwrap(),
            user_tunnel_block: "169.254.0.0/24".parse().unwrap(),
            multicastgroup_block: "239.0.0.0/24".parse().unwrap(),
            multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
            next_bgp_community: None,
        }),
        vec![
            AccountMeta::new(globalconfig_pubkey, false),
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

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
            code: "test".to_string(),
            name: "Test Location".to_string(),
            country: "us".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            lat: 0.0,
            lng: 0.0,
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

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, globalstate.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "test".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate.account_index + 1);
    let (tunnel_ids_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_block_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "test-dev".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "110.1.0.0/24".parse().unwrap(),
            metrics_publisher_pk,
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
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

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pubkey, false),
            AccountMeta::new(dz_prefix_block_pubkey, false),
        ],
        &payer,
    )
    .await;

    let client_ip: [u8; 4] = [100, 0, 0, 1];
    let (accesspass_pubkey, _) =
        get_accesspass_pda(&program_id, &client_ip.into(), &payer.pubkey());
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: client_ip.into(),
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

    let (user_pubkey, _) = get_user_pda(&program_id, &client_ip.into(), UserType::IBRL);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: client_ip.into(),
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
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

    BgpStatusTestEnv {
        banks_client,
        payer,
        program_id,
        device_pubkey,
        user_pubkey,
    }
}

fn assert_not_allowed(result: Result<(), BanksClientError>) {
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(8), // DoubleZeroError::NotAllowed
        ))) => {}
        _ => panic!("expected NotAllowed (Custom(8)), got: {:?}", result),
    }
}

// ============================================================================
// Tests
// ============================================================================

/// First write with status=Up: account resize is handled by try_acc_write automatically,
/// and all three BGP fields are set correctly.
#[tokio::test]
async fn test_bgp_status_first_write_up() {
    let BgpStatusTestEnv {
        mut banks_client,
        payer,
        program_id,
        device_pubkey,
        user_pubkey,
    } = setup().await;

    // Initial state: all BGP fields default to zero
    let user_before = get_account_data(&mut banks_client, user_pubkey)
        .await
        .unwrap()
        .get_user()
        .unwrap();
    assert_eq!(user_before.bgp_status, BGPStatus::Unknown);
    assert_eq!(user_before.last_bgp_up_at, 0);
    assert_eq!(user_before.last_bgp_reported_at, 0);

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetUserBGPStatus(SetUserBGPStatusArgs {
            bgp_status: BGPStatus::Up,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .unwrap()
        .get_user()
        .unwrap();

    assert_eq!(user.bgp_status, BGPStatus::Up);
    assert_ne!(user.last_bgp_up_at, 0, "last_bgp_up_at must be set on Up");
    assert_ne!(
        user.last_bgp_reported_at, 0,
        "last_bgp_reported_at must be set on every write"
    );
    assert_eq!(
        user.last_bgp_up_at, user.last_bgp_reported_at,
        "both timestamps must match when Up is the only write so far"
    );
}

/// First write with status=Down: last_bgp_up_at must remain zero.
#[tokio::test]
async fn test_bgp_status_first_write_down() {
    let BgpStatusTestEnv {
        mut banks_client,
        payer,
        program_id,
        device_pubkey,
        user_pubkey,
    } = setup().await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetUserBGPStatus(SetUserBGPStatusArgs {
            bgp_status: BGPStatus::Down,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .unwrap()
        .get_user()
        .unwrap();

    assert_eq!(user.bgp_status, BGPStatus::Down);
    assert_eq!(
        user.last_bgp_up_at, 0,
        "last_bgp_up_at must remain zero when the session has never been Up"
    );
    assert_ne!(
        user.last_bgp_reported_at, 0,
        "last_bgp_reported_at must be set on every write"
    );
}

/// Multiple writes: last_bgp_up_at only advances on Up transitions;
/// last_bgp_reported_at advances on every write.
#[tokio::test]
async fn test_bgp_status_multiple_writes() {
    let BgpStatusTestEnv {
        mut banks_client,
        payer,
        program_id,
        device_pubkey,
        user_pubkey,
    } = setup().await;

    // Write 1: Up
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetUserBGPStatus(SetUserBGPStatusArgs {
            bgp_status: BGPStatus::Up,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user_1 = get_account_data(&mut banks_client, user_pubkey)
        .await
        .unwrap()
        .get_user()
        .unwrap();
    let up_at_1 = user_1.last_bgp_up_at;
    let reported_at_1 = user_1.last_bgp_reported_at;
    assert_ne!(up_at_1, 0);
    assert_ne!(reported_at_1, 0);

    // Write 2: Down — last_bgp_up_at must not change; last_bgp_reported_at must advance
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetUserBGPStatus(SetUserBGPStatusArgs {
            bgp_status: BGPStatus::Down,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user_2 = get_account_data(&mut banks_client, user_pubkey)
        .await
        .unwrap()
        .get_user()
        .unwrap();
    assert_eq!(user_2.bgp_status, BGPStatus::Down);
    assert_eq!(
        user_2.last_bgp_up_at, up_at_1,
        "last_bgp_up_at must not change on a Down write"
    );
    assert!(
        user_2.last_bgp_reported_at >= reported_at_1,
        "last_bgp_reported_at must advance on every write"
    );
    let reported_at_2 = user_2.last_bgp_reported_at;

    // Write 3: Up again — last_bgp_up_at must advance; last_bgp_reported_at must advance
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetUserBGPStatus(SetUserBGPStatusArgs {
            bgp_status: BGPStatus::Up,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user_3 = get_account_data(&mut banks_client, user_pubkey)
        .await
        .unwrap()
        .get_user()
        .unwrap();
    assert_eq!(user_3.bgp_status, BGPStatus::Up);
    assert!(
        user_3.last_bgp_up_at >= up_at_1,
        "last_bgp_up_at must advance on a second Up write"
    );
    assert!(
        user_3.last_bgp_reported_at >= reported_at_2,
        "last_bgp_reported_at must advance on every write"
    );
}

/// A signer that does not match device.metrics_publisher_pk must be rejected with NotAllowed.
#[tokio::test]
async fn test_bgp_status_wrong_signer() {
    let BgpStatusTestEnv {
        mut banks_client,
        payer,
        program_id,
        device_pubkey,
        user_pubkey,
    } = setup().await;

    // Create a wrong keypair and fund it so it can pay transaction fees.
    let wrong_signer = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &wrong_signer.pubkey(),
        10_000_000,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // wrong_signer.pubkey() != device.metrics_publisher_pk (== payer.pubkey())
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetUserBGPStatus(SetUserBGPStatusArgs {
            bgp_status: BGPStatus::Up,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
        ],
        &wrong_signer,
    )
    .await;

    assert_not_allowed(result);
}

/// Passing a device whose pubkey does not match user.device_pk must be rejected with NotAllowed.
#[tokio::test]
async fn test_bgp_status_user_device_mismatch() {
    let BgpStatusTestEnv {
        mut banks_client,
        payer,
        program_id,
        device_pubkey: _device_pubkey_1,
        user_pubkey,
    } = setup().await;

    // Create a second device in the same program so that device_2 is a valid program account
    // but user.device_pk points to device_1.
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor2_pubkey, _) = get_contributor_pda(&program_id, globalstate.account_index + 1);

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "test2".to_string(),
        }),
        vec![
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let (location_pubkey, _) = get_location_pda(&program_id, 1);
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, 2);

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey_2, _) = get_device_pda(&program_id, globalstate.account_index + 1);
    let (tunnel_ids_2, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey_2, 0));
    let (dz_prefix_2, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey_2, 0));

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "dev2".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 2].into(),
            dz_prefixes: "111.1.0.0/24".parse().unwrap(),
            metrics_publisher_pk: payer.pubkey(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
            resource_count: 0,
        }),
        vec![
            AccountMeta::new(device_pubkey_2, false),
            AccountMeta::new(contributor2_pubkey, false),
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
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(device_pubkey_2, false),
            AccountMeta::new(contributor2_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey_2, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_2, false),
            AccountMeta::new(dz_prefix_2, false),
        ],
        &payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // user_pubkey.device_pk == device_1; passing device_2 must trigger NotAllowed.
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetUserBGPStatus(SetUserBGPStatusArgs {
            bgp_status: BGPStatus::Up,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey_2, false),
        ],
        &payer,
    )
    .await;

    assert_not_allowed(result);
}
