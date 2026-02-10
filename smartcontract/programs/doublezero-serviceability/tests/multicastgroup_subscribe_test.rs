use doublezero_serviceability::{
    entrypoint::process_instruction,
    instructions::*,
    pda::*,
    processors::{
        accesspass::set::SetAccessPassArgs,
        contributor::create::ContributorCreateArgs,
        device::{
            activate::DeviceActivateArgs, create::DeviceCreateArgs, update::DeviceUpdateArgs,
        },
        globalconfig::set::SetGlobalConfigArgs,
        multicastgroup::{
            activate::MulticastGroupActivateArgs,
            allowlist::{
                publisher::add::AddMulticastGroupPubAllowlistArgs,
                subscriber::add::AddMulticastGroupSubAllowlistArgs,
            },
            create::MulticastGroupCreateArgs,
            subscribe::MulticastGroupSubscribeArgs,
        },
        user::{activate::UserActivateArgs, create::UserCreateArgs},
    },
    resource::ResourceType,
    state::{
        accesspass::{AccessPass, AccessPassType},
        device::DeviceType,
        mgroup_allowlist_entry::MGroupAllowlistType,
        user::{UserCYOA, UserStatus, UserType},
    },
};
use solana_program_test::*;
use solana_sdk::{
    account::AccountSharedData, instruction::AccountMeta, pubkey::Pubkey, rent::Rent,
    signer::Signer,
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

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // 1. Init global state
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

    // 2. Set global config
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
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

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
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: "224.0.0.1".parse().unwrap(),
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
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
        }),
        vec![
            AccountMeta::new(mgroup2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: "224.0.0.2".parse().unwrap(),
        }),
        vec![
            AccountMeta::new(mgroup2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
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
        let (al_entry_pk, _) = get_mgroup_allowlist_entry_pda(
            &program_id,
            &accesspass_pubkey,
            &mgroup_pk,
            MGroupAllowlistType::Publisher as u8,
        );
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
                AccountMeta::new(al_entry_pk, false),
            ],
            &payer,
        )
        .await;
    }

    // Add both groups to sub allowlist
    for mgroup_pk in [mgroup1_pubkey, mgroup2_pubkey] {
        let (al_entry_pk, _) = get_mgroup_allowlist_entry_pda(
            &program_id,
            &accesspass_pubkey,
            &mgroup_pk,
            MGroupAllowlistType::Subscriber as u8,
        );
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
                AccountMeta::new(al_entry_pk, false),
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

/// Helper to build the accounts vec for a SubscribeMulticastGroup instruction,
/// including the two MGroupAllowlistEntry PDA accounts.
fn subscribe_accounts(
    program_id: &Pubkey,
    mgroup_pk: &Pubkey,
    accesspass_pk: &Pubkey,
    user_pk: &Pubkey,
) -> Vec<AccountMeta> {
    let (pub_al_entry, _) = get_mgroup_allowlist_entry_pda(
        program_id,
        accesspass_pk,
        mgroup_pk,
        MGroupAllowlistType::Publisher as u8,
    );
    let (sub_al_entry, _) = get_mgroup_allowlist_entry_pda(
        program_id,
        accesspass_pk,
        mgroup_pk,
        MGroupAllowlistType::Subscriber as u8,
    );
    vec![
        AccountMeta::new(*mgroup_pk, false),
        AccountMeta::new(*accesspass_pk, false),
        AccountMeta::new(*user_pk, false),
        AccountMeta::new(pub_al_entry, false),
        AccountMeta::new(sub_al_entry, false),
    ]
}

/// First publisher subscribe sets Updating (activator needs to allocate dz_ip).
/// Second publisher subscribe does NOT set Updating.
#[tokio::test]
async fn test_subscribe_first_publisher_sets_updating() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        ..
    } = f;

    // Subscribe as publisher to first group
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
        }),
        subscribe_accounts(
            &program_id,
            &mgroup1_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
        ),
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
        UserStatus::Updating,
        "First publisher subscribe should set Updating (dz_ip allocation needed)"
    );
    assert_eq!(user.publishers.len(), 1);

    let mgroup = get_account_data(&mut banks_client, mgroup1_pubkey)
        .await
        .expect("Unable to get MulticastGroup")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup.publisher_count, 1);
}

/// Second publisher subscribe should NOT set Updating since dz_ip is already allocated.
#[tokio::test]
async fn test_subscribe_second_publisher_does_not_set_updating() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        mgroup2_pubkey,
        globalstate_pubkey,
        ..
    } = f;

    // Subscribe as publisher to first group
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
        }),
        subscribe_accounts(
            &program_id,
            &mgroup1_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
        ),
        &payer,
    )
    .await;

    // Simulate activator: re-activate the user to set status back to Activated
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0,
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

    // Subscribe as publisher to second group
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
        }),
        subscribe_accounts(
            &program_id,
            &mgroup2_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
        ),
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
        "Second publisher subscribe should NOT set Updating (dz_ip already allocated)"
    );
    assert_eq!(user.publishers.len(), 2);

    let mgroup1 = get_account_data(&mut banks_client, mgroup1_pubkey)
        .await
        .expect("Unable to get MulticastGroup")
        .get_multicastgroup()
        .unwrap();
    let mgroup2 = get_account_data(&mut banks_client, mgroup2_pubkey)
        .await
        .expect("Unable to get MulticastGroup")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup1.publisher_count, 1);
    assert_eq!(mgroup2.publisher_count, 1);
}

/// Subscriber subscribe should never set Updating regardless of how many groups.
#[tokio::test]
async fn test_subscribe_subscriber_does_not_set_updating() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        mgroup2_pubkey,
        ..
    } = f;

    // Subscribe as subscriber to first group
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: true,
        }),
        subscribe_accounts(
            &program_id,
            &mgroup1_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
        ),
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
        "Subscriber subscribe should NOT set Updating"
    );
    assert_eq!(user.subscribers.len(), 1);

    // Subscribe as subscriber to second group
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: true,
        }),
        subscribe_accounts(
            &program_id,
            &mgroup2_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
        ),
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
        "Second subscriber subscribe should NOT set Updating"
    );
    assert_eq!(user.subscribers.len(), 2);

    let mgroup1 = get_account_data(&mut banks_client, mgroup1_pubkey)
        .await
        .expect("Unable to get MulticastGroup")
        .get_multicastgroup()
        .unwrap();
    let mgroup2 = get_account_data(&mut banks_client, mgroup2_pubkey)
        .await
        .expect("Unable to get MulticastGroup")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup1.subscriber_count, 1);
    assert_eq!(mgroup2.subscriber_count, 1);
}

/// Unsubscribing the last publisher should set Updating (dz_ip no longer needed).
#[tokio::test]
async fn test_unsubscribe_last_publisher_sets_updating() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        mgroup2_pubkey,
        globalstate_pubkey,
        ..
    } = f;

    // Subscribe as publisher to both groups
    for mgroup_pk in [mgroup1_pubkey, mgroup2_pubkey] {
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
                client_ip: [100, 0, 0, 1].into(),
                publisher: true,
                subscriber: false,
            }),
            subscribe_accounts(&program_id, &mgroup_pk, &accesspass_pubkey, &user_pubkey),
            &payer,
        )
        .await;
    }

    // Simulate activator: re-activate
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0,
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
    assert_eq!(user.publishers.len(), 2);

    // Unsubscribe from first group (still have second)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: false,
        }),
        subscribe_accounts(
            &program_id,
            &mgroup1_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
        ),
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
        "Unsubscribing non-last publisher should NOT set Updating"
    );
    assert_eq!(user.publishers.len(), 1);

    // Unsubscribe from second group (now publishers is empty)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: false,
            subscriber: false,
        }),
        subscribe_accounts(
            &program_id,
            &mgroup2_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
        ),
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
        UserStatus::Updating,
        "Unsubscribing last publisher should set Updating (dz_ip no longer needed)"
    );
    assert_eq!(user.publishers.len(), 0);
}

/// Duplicate publisher subscribe should be a no-op (no status change, no count increment).
#[tokio::test]
async fn test_duplicate_publisher_subscribe_is_noop() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        globalstate_pubkey,
        ..
    } = f;

    // Subscribe as publisher
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
        }),
        subscribe_accounts(
            &program_id,
            &mgroup1_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
        ),
        &payer,
    )
    .await;

    // Re-activate to reset to Activated
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Subscribe again to same group
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
        }),
        subscribe_accounts(
            &program_id,
            &mgroup1_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
        ),
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
        "Duplicate subscribe should not change status"
    );
    assert_eq!(user.publishers.len(), 1, "Should not duplicate publisher");

    let mgroup = get_account_data(&mut banks_client, mgroup1_pubkey)
        .await
        .expect("Unable to get MulticastGroup")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(
        mgroup.publisher_count, 1,
        "Should not double-count publisher"
    );
}

/// Setup fixture with Vec-based allowlist entries (legacy) instead of PDA entries.
/// Uses ProgramTestContext so we can inject Vec entries via set_account.
async fn setup_vec_fixture() -> (
    ProgramTestContext,
    Pubkey,
    Pubkey,
    Pubkey,
    Pubkey,
    Pubkey,
    Pubkey,
) {
    let program_id = Pubkey::new_unique();
    let mut context = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start_with_context()
    .await;

    let payer_pk = context.payer.pubkey();
    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // 1. Init global state
    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &context.payer,
    )
    .await;

    // 2. Set global config
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
        &mut context.banks_client,
        context.last_blockhash,
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
        &context.payer,
    )
    .await;

    // 3. Create location
    let gs = get_globalstate(&mut context.banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
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
        &context.payer,
    )
    .await;

    // 4. Create exchange
    let gs = get_globalstate(&mut context.banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
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
        &context.payer,
    )
    .await;

    // 5. Create contributor
    let gs = get_globalstate(&mut context.banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "cont".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(payer_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &context.payer,
    )
    .await;

    // 6. Create and activate device
    let gs = get_globalstate(&mut context.banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
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
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &context.payer,
    )
    .await;

    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
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
        &context.payer,
    )
    .await;

    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &context.payer,
    )
    .await;

    // 7. Create two multicast groups and activate them
    let gs = get_globalstate(&mut context.banks_client, globalstate_pubkey).await;
    let (mgroup1_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "group1".to_string(),
            max_bandwidth: 1000,
            owner: payer_pk,
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &context.payer,
    )
    .await;

    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: "224.0.0.1".parse().unwrap(),
        }),
        vec![
            AccountMeta::new(mgroup1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &context.payer,
    )
    .await;

    let gs = get_globalstate(&mut context.banks_client, globalstate_pubkey).await;
    let (mgroup2_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "group2".to_string(),
            max_bandwidth: 1000,
            owner: payer_pk,
        }),
        vec![
            AccountMeta::new(mgroup2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &context.payer,
    )
    .await;

    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: "224.0.0.2".parse().unwrap(),
        }),
        vec![
            AccountMeta::new(mgroup2_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &context.payer,
    )
    .await;

    // 8. Create access pass (NO PDA allowlist entries — we'll inject Vec entries instead)
    let user_ip: Ipv4Addr = [100, 0, 0, 1].into();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user_ip, &payer_pk);

    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
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
            AccountMeta::new(payer_pk, false),
        ],
        &context.payer,
    )
    .await;

    // Inject Vec-based allowlist entries into the AccessPass account
    let acc = context
        .banks_client
        .get_account(accesspass_pubkey)
        .await
        .unwrap()
        .unwrap();
    let mut accesspass = AccessPass::try_from(&acc.data[..]).unwrap();
    accesspass.mgroup_pub_allowlist = vec![mgroup1_pubkey, mgroup2_pubkey];
    accesspass.mgroup_sub_allowlist = vec![mgroup1_pubkey, mgroup2_pubkey];
    let new_data = borsh::to_vec(&accesspass).unwrap();
    let rent = Rent::default();
    let mut shared = AccountSharedData::new(
        rent.minimum_balance(new_data.len()).max(acc.lamports),
        new_data.len(),
        &program_id,
    );
    shared.set_data_from_slice(&new_data);
    context.set_account(&accesspass_pubkey, &shared);

    // 9. Create user (Multicast type) and activate
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);
    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &context.payer,
    )
    .await;

    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            dz_ip: user_ip,
            dz_prefix_count: 0,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &context.payer,
    )
    .await;

    (
        context,
        program_id,
        globalstate_pubkey,
        accesspass_pubkey,
        user_pubkey,
        mgroup1_pubkey,
        mgroup2_pubkey,
    )
}

/// Subscribe with Vec-based allowlist should self-migrate: create PDA and clear Vec entry.
#[tokio::test]
async fn test_subscribe_vec_fallback_creates_pda_and_clears_vec() {
    let (mut context, program_id, _, accesspass_pubkey, user_pubkey, mgroup1_pubkey, _) =
        setup_vec_fixture().await;

    // Verify AccessPass has Vec entries before subscribe
    let acc = context
        .banks_client
        .get_account(accesspass_pubkey)
        .await
        .unwrap()
        .unwrap();
    let ap = AccessPass::try_from(&acc.data[..]).unwrap();
    assert_eq!(ap.mgroup_pub_allowlist.len(), 2);
    assert_eq!(ap.mgroup_sub_allowlist.len(), 2);

    // Verify the PDA allowlist entry does NOT exist yet
    let (pub_al_entry_pk, _) = get_mgroup_allowlist_entry_pda(
        &program_id,
        &accesspass_pubkey,
        &mgroup1_pubkey,
        MGroupAllowlistType::Publisher as u8,
    );
    let pda_account = context
        .banks_client
        .get_account(pub_al_entry_pk)
        .await
        .unwrap();
    assert!(
        pda_account.is_none(),
        "PDA should not exist before subscribe"
    );

    // Subscribe as publisher to mgroup1 (should trigger Vec fallback + self-migration)
    execute_transaction(
        &mut context.banks_client,
        context.last_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
        }),
        subscribe_accounts(
            &program_id,
            &mgroup1_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
        ),
        &context.payer,
    )
    .await;

    // Verify the PDA was created
    let pda_account = context
        .banks_client
        .get_account(pub_al_entry_pk)
        .await
        .unwrap();
    assert!(
        pda_account.is_some(),
        "PDA should be created by self-migration"
    );

    // Verify the Vec entry was removed (mgroup1 migrated, mgroup2 still in Vec)
    let acc = context
        .banks_client
        .get_account(accesspass_pubkey)
        .await
        .unwrap()
        .unwrap();
    let ap = AccessPass::try_from(&acc.data[..]).unwrap();
    assert_eq!(
        ap.mgroup_pub_allowlist.len(),
        1,
        "mgroup1 should be removed from Vec after migration"
    );
    assert!(
        !ap.mgroup_pub_allowlist.contains(&mgroup1_pubkey),
        "mgroup1 should no longer be in Vec"
    );
    assert!(
        !ap.mgroup_pub_allowlist.contains(&mgroup1_pubkey),
        "mgroup2 should remain in Vec"
    );

    // Verify user got the publisher
    let user = get_account_data(&mut context.banks_client, user_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user.publishers.len(), 1);
    assert!(user.publishers.contains(&mgroup1_pubkey));
}

/// Subscribe without any allowlist entry (neither PDA nor Vec) should fail.
#[tokio::test]
async fn test_subscribe_not_allowed_without_allowlist() {
    let f = setup_fixture().await;
    let TestFixture {
        mut banks_client,
        payer,
        program_id,
        recent_blockhash,
        globalstate_pubkey,
        accesspass_pubkey,
        user_pubkey,
        ..
    } = f;

    // Create a 3rd multicast group (not in any allowlist)
    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup3_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "group3".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(mgroup3_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: "224.0.0.3".parse().unwrap(),
        }),
        vec![
            AccountMeta::new(mgroup3_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Try to subscribe to mgroup3 without any allowlist entry — should fail
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
            client_ip: [100, 0, 0, 1].into(),
            publisher: true,
            subscriber: false,
        }),
        subscribe_accounts(
            &program_id,
            &mgroup3_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
        ),
        &payer,
    )
    .await;

    assert!(
        res.is_err(),
        "Subscribe should fail when group is not in any allowlist"
    );
}
