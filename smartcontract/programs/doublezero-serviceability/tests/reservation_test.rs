use doublezero_serviceability::{
    entrypoint::*,
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{activate::DeviceActivateArgs, create::*, update::*},
        globalstate::setauthority::SetAuthorityArgs,
        multicastgroup::{activate::MulticastGroupActivateArgs, create::MulticastGroupCreateArgs},
        reservation::{close::CloseReservationArgs, reserve::ReserveConnectionArgs},
        user::create_reserved_subscribe::CreateReservedSubscribeUserArgs,
        *,
    },
    resource::ResourceType,
    state::{
        accounttype::AccountType,
        device::*,
        multicastgroup::MulticastGroup,
        reservation::Reservation,
        user::{User, UserCYOA, UserStatus, UserType},
    },
};
use globalconfig::set::SetGlobalConfigArgs;
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signer::Signer,
    transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

/// Setup a full program environment with an activated device ready for reservation tests.
/// Returns (banks_client, payer, program_id, globalstate_pubkey, device_pubkey)
async fn setup_device_for_reservations(
    max_users: u16,
) -> (
    BanksClient,
    solana_sdk::signature::Keypair,
    Pubkey,
    Pubkey,
    Pubkey,
) {
    let program_id = Pubkey::new_unique();
    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

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

    // Set global config
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
        ],
        &payer,
    )
    .await;

    // Set reservation authority to payer
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            activator_authority_pk: None,
            sentinel_authority_pk: None,
            health_oracle_pk: None,
            reservation_authority_pk: Some(payer.pubkey()),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

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
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "dev1".to_string(),
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

    // Set max_users
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_users: Some(max_users),
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

    (
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
    )
}

async fn get_reservation(banks_client: &mut BanksClient, pubkey: Pubkey) -> Option<Reservation> {
    match banks_client.get_account(pubkey).await {
        Ok(account) => match account {
            Some(account_data) => Reservation::try_from(&account_data.data[..]).ok(),
            None => None,
        },
        Err(err) => panic!("Failed to get account: {err:?}"),
    }
}

#[tokio::test]
async fn test_reserve_connection() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 5 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify reservation account exists with correct data
    let reservation = get_reservation(&mut banks_client, reservation_pubkey)
        .await
        .expect("Reservation account should exist");
    assert_eq!(reservation.account_type, AccountType::Reservation);
    assert_eq!(reservation.device_pk, device_pubkey);
    assert_eq!(reservation.reserved_count, 5);
    assert_eq!(reservation.owner, payer.pubkey());

    // Verify device.reserved_seats incremented by count
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");
    assert_eq!(device.reserved_seats, 5);
}

#[tokio::test]
async fn test_reserve_connection_count_zero() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    // Reserving 0 seats should fail with InvalidArgument
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 0 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // InvalidArgument = Custom(65)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(65),
        ))) => {}
        _ => panic!(
            "Expected InvalidArgument error (Custom(65)), got {:?}",
            result
        ),
    }
}

#[tokio::test]
async fn test_reserve_connection_at_capacity() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(5).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    // Reserve all 5 seats
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 5 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // A second reservation by a different authority should fail — device at capacity
    let other_authority = solana_sdk::signature::Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &other_authority.pubkey(),
        10_000_000,
    )
    .await;

    // Add other_authority to foundation allowlist so it's authorized
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddFoundationAllowlist(
            allowlist::foundation::add::AddFoundationAllowlistArgs {
                pubkey: other_authority.pubkey(),
            },
        ),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let (reservation_pubkey_2, _) =
        get_reservation_pda(&program_id, &device_pubkey, &other_authority.pubkey());
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 1 }),
        vec![
            AccountMeta::new(reservation_pubkey_2, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &other_authority,
    )
    .await;

    // MaxUsersExceeded = Custom(20)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(20),
        ))) => {}
        _ => panic!(
            "Expected MaxUsersExceeded error (Custom(20)), got {:?}",
            result
        ),
    }
}

#[tokio::test]
async fn test_reserve_connection_exceeds_capacity() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(3).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    // Reserving more seats than available should fail
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 4 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // MaxUsersExceeded = Custom(20)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(20),
        ))) => {}
        _ => panic!(
            "Expected MaxUsersExceeded error (Custom(20)), got {:?}",
            result
        ),
    }
}

#[tokio::test]
async fn test_reserve_connection_at_capacity_with_users_count() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(5).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Set users_count=3 so only 2 seats remain
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            users_count: Some(3),
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(device.contributor_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    // Reserving 3 seats should fail: users_count(3) + count(3) > max_users(5)
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 3 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // MaxUsersExceeded = Custom(20)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(20),
        ))) => {}
        _ => panic!(
            "Expected MaxUsersExceeded error (Custom(20)), got {:?}",
            result
        ),
    }
}

#[tokio::test]
async fn test_close_reservation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    // Reserve 5 seats
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 5 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");
    assert_eq!(device.reserved_seats, 5);

    // Close reservation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseReservation(CloseReservationArgs {}),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify reservation account is closed
    let reservation = get_reservation(&mut banks_client, reservation_pubkey).await;
    assert!(
        reservation.is_none(),
        "Reservation account should be closed"
    );

    // Verify device.reserved_seats decremented by full count
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");
    assert_eq!(device.reserved_seats, 0);
}

#[tokio::test]
async fn test_reserve_connection_unauthorized() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create a second keypair that is NOT the reservation authority and NOT in foundation_allowlist
    let unauthorized = solana_sdk::signature::Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &unauthorized.pubkey(),
        10_000_000,
    )
    .await;

    let (reservation_pubkey, _) =
        get_reservation_pda(&program_id, &device_pubkey, &unauthorized.pubkey());

    // Reserve signed by unauthorized keypair should fail with NotAllowed
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 1 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &unauthorized,
    )
    .await;

    // NotAllowed = Custom(8)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(8),
        ))) => {}
        _ => panic!("Expected NotAllowed error (Custom(8)), got {:?}", result),
    }
}

#[tokio::test]
async fn test_double_reserve_same_owner() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    // First reservation succeeds
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 3 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Second reservation for same (device, owner) should fail — PDA already exists
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 2 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "Double reservation for same (device, owner) should fail"
    );
}

#[tokio::test]
async fn test_reserve_connection_max_users_zero() {
    // A device with max_users=0 is locked — reservations should be rejected
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(0).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 1 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // MaxUsersExceeded = Custom(20)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(20),
        ))) => {}
        _ => panic!(
            "Expected MaxUsersExceeded error (Custom(20)), got {:?}",
            result
        ),
    }
}

#[tokio::test]
async fn test_reserve_connection_respects_max_multicast_subscribers() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    // Set max_multicast_subscribers=2 (lower than max_users=128)
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_multicast_subscribers: Some(2),
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(device.contributor_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    // Reserve 2 seats — should succeed (at multicast limit)
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 2 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Another reservation should fail — multicast capacity reached
    let other_authority = solana_sdk::signature::Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &other_authority.pubkey(),
        10_000_000,
    )
    .await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddFoundationAllowlist(
            allowlist::foundation::add::AddFoundationAllowlistArgs {
                pubkey: other_authority.pubkey(),
            },
        ),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let (reservation_pubkey_2, _) =
        get_reservation_pda(&program_id, &device_pubkey, &other_authority.pubkey());
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 1 }),
        vec![
            AccountMeta::new(reservation_pubkey_2, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &other_authority,
    )
    .await;

    // MaxMulticastSubscribersExceeded = Custom(82)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(82),
        ))) => {}
        _ => panic!(
            "Expected MaxMulticastSubscribersExceeded (Custom(82)), got {:?}",
            result
        ),
    }
}

// --- CreateReservedSubscribeUser instruction tests ---

async fn get_user(banks_client: &mut BanksClient, pubkey: Pubkey) -> Option<User> {
    match banks_client.get_account(pubkey).await {
        Ok(account) => match account {
            Some(account_data) => User::try_from(&account_data.data[..]).ok(),
            None => None,
        },
        Err(err) => panic!("Failed to get account: {err:?}"),
    }
}

async fn get_multicast_group(
    banks_client: &mut BanksClient,
    pubkey: Pubkey,
) -> Option<MulticastGroup> {
    match banks_client.get_account(pubkey).await {
        Ok(account) => match account {
            Some(account_data) => MulticastGroup::try_from(&account_data.data[..]).ok(),
            None => None,
        },
        Err(err) => panic!("Failed to get account: {err:?}"),
    }
}

/// Create and activate a multicast group. Returns its pubkey.
async fn setup_multicast_group(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
) -> Pubkey {
    let gs = get_globalstate(banks_client, globalstate_pubkey).await;
    let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "group1".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: "224.0.0.1".parse().unwrap(),
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    mgroup_pubkey
}

#[tokio::test]
async fn test_create_reserved_subscribe_user() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    let mgroup_pubkey =
        setup_multicast_group(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    // Reserve 3 seats
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 3 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user_ip: std::net::Ipv4Addr = [100, 1, 0, 1].into();
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateReservedSubscribeUser(CreateReservedSubscribeUserArgs {
            client_ip: user_ip,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify reservation count decremented
    let reservation = get_reservation(&mut banks_client, reservation_pubkey)
        .await
        .expect("Reservation should still exist");
    assert_eq!(reservation.reserved_count, 2);

    // Verify device counters
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");
    assert_eq!(device.reserved_seats, 2);
    assert_eq!(device.users_count, 1);
    assert_eq!(device.reference_count, 1);
    assert_eq!(device.multicast_subscribers_count, 1);

    // Verify user
    let user = get_user(&mut banks_client, user_pubkey)
        .await
        .expect("User should exist");
    assert_eq!(user.client_ip, user_ip);
    assert_eq!(user.user_type, UserType::Multicast);
    assert_eq!(user.device_pk, device_pubkey);
    assert_eq!(user.tenant_pk, Pubkey::default());
    assert_eq!(user.status, UserStatus::Pending);
    assert_eq!(user.owner, payer.pubkey());
    assert_eq!(user.subscribers, vec![mgroup_pubkey]);
    assert!(user.publishers.is_empty());

    // Verify multicast group subscriber count
    let mgroup = get_multicast_group(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup should exist");
    assert_eq!(mgroup.subscriber_count, 1);
    assert_eq!(mgroup.publisher_count, 0);
}

#[tokio::test]
async fn test_create_reserved_subscribe_user_exhausted() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    let mgroup_pubkey =
        setup_multicast_group(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 1 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create first user — consumes the only reserved seat
    let user_ip_1: std::net::Ipv4Addr = [100, 1, 0, 1].into();
    let (user_pubkey_1, _) = get_user_pda(&program_id, &user_ip_1, UserType::Multicast);

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateReservedSubscribeUser(CreateReservedSubscribeUserArgs {
            client_ip: user_ip_1,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey_1, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert_eq!(
        get_reservation(&mut banks_client, reservation_pubkey)
            .await
            .unwrap()
            .reserved_count,
        0
    );

    // Second user should fail — reservation exhausted
    let user_ip_2: std::net::Ipv4Addr = [100, 1, 0, 2].into();
    let (user_pubkey_2, _) = get_user_pda(&program_id, &user_ip_2, UserType::Multicast);

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateReservedSubscribeUser(CreateReservedSubscribeUserArgs {
            client_ip: user_ip_2,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey_2, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(20),
        ))) => {}
        _ => panic!("Expected MaxUsersExceeded (Custom(20)), got {:?}", result),
    }
}

#[tokio::test]
async fn test_create_reserved_subscribe_user_wrong_owner() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    let mgroup_pubkey =
        setup_multicast_group(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 3 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Another authority tries to use payer's reservation
    let other_user = solana_sdk::signature::Keypair::new();
    transfer(&mut banks_client, &payer, &other_user.pubkey(), 10_000_000).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddFoundationAllowlist(
            allowlist::foundation::add::AddFoundationAllowlistArgs {
                pubkey: other_user.pubkey(),
            },
        ),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let user_ip: std::net::Ipv4Addr = [100, 1, 0, 1].into();
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateReservedSubscribeUser(CreateReservedSubscribeUserArgs {
            client_ip: user_ip,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &other_user,
    )
    .await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(8),
        ))) => {}
        _ => panic!("Expected NotAllowed (Custom(8)), got {:?}", result),
    }
}

#[tokio::test]
async fn test_create_reserved_subscribe_user_wrong_device() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    let mgroup_pubkey =
        setup_multicast_group(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 3 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create a second device
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, 1);
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, 2);
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, 3);
    let (device2_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "dev2".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 2].into(),
            dz_prefixes: "100.2.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 0,
        }),
        vec![
            AccountMeta::new(device2_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Activate device2
    let (tunnel_ids_pda2, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device2_pubkey, 0));
    let (dz_prefix_pda2, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device2_pubkey, 0));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pda2, false),
            AccountMeta::new(dz_prefix_pda2, false),
        ],
        &payer,
    )
    .await;

    // Try to create user on device2 using reservation for device1
    let user_ip: std::net::Ipv4Addr = [100, 1, 0, 1].into();
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateReservedSubscribeUser(CreateReservedSubscribeUserArgs {
            client_ip: user_ip,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device2_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // InvalidDevicePubkey = Custom(6)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(6),
        ))) => {}
        _ => panic!("Expected InvalidDevicePubkey (Custom(6)), got {:?}", result),
    }
}

#[tokio::test]
async fn test_create_reserved_subscribe_user_unauthorized() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    let mgroup_pubkey =
        setup_multicast_group(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 3 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Unauthorized user (not reservation authority, not in foundation allowlist)
    let unauthorized = solana_sdk::signature::Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &unauthorized.pubkey(),
        10_000_000,
    )
    .await;

    let user_ip: std::net::Ipv4Addr = [100, 1, 0, 1].into();
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateReservedSubscribeUser(CreateReservedSubscribeUserArgs {
            client_ip: user_ip,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &unauthorized,
    )
    .await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(8),
        ))) => {}
        _ => panic!("Expected NotAllowed (Custom(8)), got {:?}", result),
    }
}

#[tokio::test]
async fn test_create_reserved_subscribe_user_multiple_from_same_reservation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    let mgroup_pubkey =
        setup_multicast_group(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 3 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create 3 users from the same reservation
    for i in 1..=3u8 {
        let user_ip: std::net::Ipv4Addr = [100, 1, 0, i].into();
        let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);

        let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateReservedSubscribeUser(CreateReservedSubscribeUserArgs {
                client_ip: user_ip,
                user_type: UserType::Multicast,
                cyoa_type: UserCYOA::GREOverDIA,
                tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
                publisher: false,
                subscriber: true,
                dz_prefix_count: 0,
            }),
            vec![
                AccountMeta::new(user_pubkey, false),
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(mgroup_pubkey, false),
                AccountMeta::new(reservation_pubkey, false),
                AccountMeta::new_readonly(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;
    }

    let reservation = get_reservation(&mut banks_client, reservation_pubkey)
        .await
        .expect("Reservation should still exist");
    assert_eq!(reservation.reserved_count, 0);

    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device should exist");
    assert_eq!(device.reserved_seats, 0);
    assert_eq!(device.users_count, 3);
    assert_eq!(device.reference_count, 3);
    assert_eq!(device.multicast_subscribers_count, 3);

    let mgroup = get_multicast_group(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup should exist");
    assert_eq!(mgroup.subscriber_count, 3);
}

#[tokio::test]
async fn test_create_reserved_subscribe_user_rejects_non_multicast() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, device_pubkey) =
        setup_device_for_reservations(128).await;

    let mgroup_pubkey =
        setup_multicast_group(&mut banks_client, &payer, program_id, globalstate_pubkey).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (reservation_pubkey, _) = get_reservation_pda(&program_id, &device_pubkey, &payer.pubkey());

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReserveConnection(ReserveConnectionArgs { count: 1 }),
        vec![
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user_ip: std::net::Ipv4Addr = [100, 1, 0, 1].into();
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::IBRL);

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateReservedSubscribeUser(CreateReservedSubscribeUserArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(reservation_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // InvalidArgument = Custom(65)
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(65),
        ))) => {}
        _ => panic!("Expected InvalidArgument (Custom(65)), got {:?}", result),
    }
}
