use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::set::SetAccessPassArgs,
        allowlist::foundation::add::AddFoundationAllowlistArgs,
        contributor::create::ContributorCreateArgs,
        device::{
            activate::DeviceActivateArgs, create::DeviceCreateArgs, update::DeviceUpdateArgs,
        },
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        multicastgroup::{
            allowlist::{
                publisher::add::AddMulticastGroupPubAllowlistArgs,
                subscriber::add::AddMulticastGroupSubAllowlistArgs,
            },
            create::MulticastGroupCreateArgs,
            subscribe::UpdateMulticastGroupRolesArgs,
        },
        user::{activate::UserActivateArgs, create::UserCreateArgs},
    },
    resource::ResourceType,
    state::{
        accesspass::AccessPassType,
        device::DeviceType,
        feature_flags::FeatureFlag,
        user::{UserCYOA, UserStatus, UserType},
    },
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signer::Signer,
    transaction::TransactionError,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

/// Helper: bootstrap global state, config, location, exchange, contributor, device, accesspass,
/// and two activated multicast groups. Returns all the pubkeys needed for subscribe tests.
struct TestFixture {
    banks_client: BanksClient,
    payer: solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    _device_pubkey: Pubkey,
    accesspass_pubkey: Pubkey,
    user_pubkey: Pubkey,
    mgroup1_pubkey: Pubkey,
    mgroup2_pubkey: Pubkey,
    _user_ip: Ipv4Addr,
    recent_blockhash: solana_program::hash::Hash,
}

async fn setup_fixture() -> TestFixture {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // 1. Init global state
    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

    // 2. Set global config
    let (config_pubkey, _) = get_globalconfig_pda(&program_id);
    let (_link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (_multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (_vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);

    // 3. Create location
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(
            doublezero_serviceability::processors::location::create::LocationCreateArgs {
                code: "la".to_string(),
                name: "Los Angeles".to_string(),
                country: "us".to_string(),
                lat: 1.0,
                lng: 2.0,
                loc_id: 0,
            },
        ),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // 4. Create exchange
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(
            doublezero_serviceability::processors::exchange::create::ExchangeCreateArgs {
                code: "la".to_string(),
                name: "Los Angeles".to_string(),
                lat: 1.0,
                lng: 2.0,
                reserved: 0,
            },
        ),
        vec![
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // 5. Create contributor
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

    // 6. Create and activate device
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
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "la".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "100.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            ),
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
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    // 7. Create two multicast groups and activate them
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup1_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "group1".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup2_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "group2".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
        ],
        &payer,
    )
    .await;

    // 8. Create access pass with both groups in pub+sub allowlists
    let user_ip: Ipv4Addr = [100, 0, 0, 1].into();
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
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Add both groups to pub allowlist
    for mgroup_pk in [mgroup1_pubkey, mgroup2_pubkey] {
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AddMulticastGroupPubAllowlist(
                AddMulticastGroupPubAllowlistArgs {
                    client_ip: user_ip,
                    user_payer: payer.pubkey(),
                },
            ),
            vec![
                AccountMeta::new(mgroup_pk, false),
                AccountMeta::new(accesspass_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;
    }

    // Add both groups to sub allowlist
    for mgroup_pk in [mgroup1_pubkey, mgroup2_pubkey] {
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                AddMulticastGroupSubAllowlistArgs {
                    client_ip: user_ip,
                    user_payer: payer.pubkey(),
                },
            ),
            vec![
                AccountMeta::new(mgroup_pk, false),
                AccountMeta::new(accesspass_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;
    }

    // 9. Create user (Multicast type) and activate
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::Multicast,
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

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            dz_ip: user_ip,
            dz_prefix_count: 0,
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

    TestFixture {
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        _device_pubkey: device_pubkey,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        mgroup2_pubkey,
        _user_ip: user_ip,
        recent_blockhash,
    }
}

/// Foundation admin (payer != user.owner) can subscribe a user to a multicast group.
/// Regression test for the bug where process_update_multicastgroup_roles derived the AccessPass PDA
/// using payer_account.key instead of user.owner.
#[tokio::test]
async fn test_subscribe_foundation_admin_payer_differs_from_user_owner() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer, // payer is user.owner (alice)
        program_id,
        accesspass_pubkey, // derived from payer.pubkey() = user.owner
        user_pubkey,
        mgroup1_pubkey,
        globalstate_pubkey,
        ..
    } = f;

    // foundation_admin is a different keypair — simulates a foundation admin acting on behalf of the user
    let foundation_admin = solana_sdk::signature::Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &foundation_admin.pubkey(),
        10_000_000,
    )
    .await;

    // Add foundation_admin to the foundation allowlist so they can act on behalf of users.
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddFoundationAllowlist(AddFoundationAllowlistArgs {
            pubkey: foundation_admin.pubkey(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // Subscribe the user as subscriber, signed by foundation_admin (payer != user.owner).
    // Before the fix this would panic with "Invalid AccessPass PDA" because the PDA was
    // derived from payer_account.key (foundation_admin) instead of user.owner (alice).
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: true,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock).0,
                false,
            ),
        ],
        &foundation_admin,
    )
    .await
    .expect("Foundation admin should be able to subscribe user to multicast group");

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.subscribers.len(), 1);
    assert_eq!(user.status, UserStatus::Activated);
}

/// Foundation admin (payer != user.owner) can unsubscribe a user from a multicast group.
/// This is the same regression as test_subscribe_foundation_admin_payer_differs_from_user_owner
/// but exercises the unsubscribe direction (publisher: false, subscriber: false), which is the
/// exact path taken by `user delete` when clearing subscriptions before deleting the user.
#[tokio::test]
async fn test_unsubscribe_foundation_admin_payer_differs_from_user_owner() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer, // payer is user.owner (alice)
        program_id,
        accesspass_pubkey, // derived from payer.pubkey() = user.owner
        user_pubkey,
        mgroup1_pubkey,
        globalstate_pubkey,
        ..
    } = f;

    // First subscribe the user as user.owner (alice) so there is a subscription to remove.
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: true,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock).0,
                false,
            ),
        ],
        &payer,
    )
    .await
    .expect("user.owner should be able to subscribe");

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.subscribers.len(), 1);

    // foundation_admin is a different keypair — simulates a foundation admin deleting the user.
    let foundation_admin = solana_sdk::signature::Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &foundation_admin.pubkey(),
        10_000_000,
    )
    .await;

    // Add foundation_admin to the foundation allowlist so they can act on behalf of users.
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddFoundationAllowlist(AddFoundationAllowlistArgs {
            pubkey: foundation_admin.pubkey(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // Unsubscribe the user, signed by foundation_admin (payer != user.owner).
    // Before the fix this would panic with "Invalid AccessPass PDA" because the PDA was
    // derived from payer_account.key (foundation_admin) instead of user.owner (alice).
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock).0,
                false,
            ),
        ],
        &foundation_admin,
    )
    .await
    .expect("Foundation admin should be able to unsubscribe user from multicast group");

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.subscribers.len(), 0);
    assert_eq!(user.status, UserStatus::Activated);
}

/// A payer who is not the access pass owner and not in the foundation allowlist is rejected.
#[tokio::test]
async fn test_subscribe_unauthorized_payer_rejected() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        globalstate_pubkey,
        ..
    } = f;

    let other_payer = solana_sdk::signature::Keypair::new();
    transfer(&mut banks_client, &payer, &other_payer.pubkey(), 10_000_000).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: true,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock).0,
                false,
            ),
        ],
        &other_payer,
    )
    .await;

    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(22),
        ))) => {}
        _ => panic!("Expected Unauthorized error (Custom(22)), got {:?}", result),
    }
}

// --- Onchain allocation tests ---

/// Helper to enable the OnChainAllocation feature flag on an existing fixture.
async fn enable_onchain_allocation(
    banks_client: &mut BanksClient,
    recent_blockhash: solana_program::hash::Hash,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &solana_sdk::signature::Keypair,
) {
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        payer,
    )
    .await;
}

/// First publisher subscribe with onchain allocation allocates dz_ip directly,
/// user stays Activated (no Updating round-trip).
#[tokio::test]
async fn test_subscribe_onchain_first_publisher_allocates_dz_ip() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        globalstate_pubkey,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        ..
    } = f;

    enable_onchain_allocation(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        &payer,
    )
    .await;

    let user_before = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    let dz_ip_before = user_before.dz_ip;

    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);

    // Subscribe as publisher with onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(
        user.status,
        UserStatus::Activated,
        "Onchain allocation should keep user Activated (no Updating)"
    );
    assert_eq!(user.publishers.len(), 1);
    assert_ne!(
        user.dz_ip, dz_ip_before,
        "dz_ip should have been allocated from MulticastPublisherBlock"
    );
    assert_ne!(
        user.dz_ip,
        Ipv4Addr::UNSPECIFIED,
        "dz_ip should not be UNSPECIFIED"
    );
    assert_ne!(
        user.dz_ip, user.client_ip,
        "dz_ip should differ from client_ip"
    );
}

/// Subscriber subscribe with onchain allocation should not allocate dz_ip.
#[tokio::test]
async fn test_subscribe_onchain_subscriber_no_allocation() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        globalstate_pubkey,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        ..
    } = f;

    enable_onchain_allocation(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        &payer,
    )
    .await;

    let user_before = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();

    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);

    // Subscribe as subscriber only with onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: true,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
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
    assert_eq!(user.subscribers.len(), 1);
    assert_eq!(
        user.dz_ip, user_before.dz_ip,
        "dz_ip should not change for subscriber-only subscription"
    );
}

/// Second publisher subscribe with onchain allocation should not reallocate dz_ip.
#[tokio::test]
async fn test_subscribe_onchain_second_publisher_no_reallocation() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        globalstate_pubkey,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        mgroup2_pubkey,
        ..
    } = f;

    enable_onchain_allocation(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        &payer,
    )
    .await;

    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);

    // Subscribe as publisher to first group with onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
        ],
        &payer,
    )
    .await;

    let user_after_first = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    let dz_ip_after_first = user_after_first.dz_ip;
    assert_ne!(dz_ip_after_first, Ipv4Addr::UNSPECIFIED);

    // Subscribe as publisher to second group with onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup2_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
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
    assert_eq!(user.publishers.len(), 2);
    assert_eq!(
        user.dz_ip, dz_ip_after_first,
        "dz_ip should not change on second publisher subscription"
    );
}
