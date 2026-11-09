//! `UpdateFeedSubscription` (variant 117) — joining and leaving whole feeds on an EdgeSeat pass.
//!
//! The property under test throughout is that a seat is held per *feed*, not per group: three
//! groups inside one feed cost one seat, a second feed costs a second, and a seat is released only
//! when the user's last group in that feed goes away.

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_device_pda, get_feed_pda, get_multicastgroup_pda,
        get_resource_extension_pda, get_user_pda,
    },
    processors::{
        accesspass::{
            set::SetAccessPassArgs,
            set_feeds::{FeedSeatConfig, SetAccessPassFeedsArgs},
        },
        device::{
            create::DeviceCreateArgs, interface::create::DeviceInterfaceCreateArgs,
            update::DeviceUpdateArgs,
        },
        feed::create::{FeedCreateArgs, MAX_FEED_GROUPS},
        multicastgroup::{
            create::MulticastGroupCreateArgs,
            subscribe::UpdateMulticastGroupRolesArgs,
            subscribe_feed::{SubscribeFeedArgs, MAX_USER_FEEDS},
            unsubscribe_feed::UnsubscribeFeedArgs,
            update::MulticastGroupUpdateArgs,
        },
        permission::create::PermissionCreateArgs,
        user::{
            create::UserCreateArgs, create_subscribe::UserCreateSubscribeArgs,
            delete::UserDeleteArgs,
        },
    },
    resource::ResourceType,
    state::{
        accesspass::{AccessPass, AccessPassKind, AccessPassType, FeedSeat},
        device::DeviceType,
        interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
        permission::permission_flags,
        user::{User, UserCYOA, UserType},
    },
};
use doublezero_serviceability_instruction as dzi;
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, Instruction, InstructionError},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::{Transaction, TransactionError},
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

// Far-future billing-window bounds so the "window_end must be in the future" check stays satisfied.
const TEST_WINDOW_END: i64 = 4_000_000_000;
const TEST_TERMINATES_AT: i64 = 4_100_000_000;

struct Fixture {
    banks_client: BanksClient,
    payer: solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    exchange_pubkey: Pubkey,
    contributor_pubkey: Pubkey,
    device_pubkey: Pubkey,
    accesspass_pubkey: Pubkey,
    /// Five activated multicast groups, split across feeds by the tests.
    groups: Vec<Pubkey>,
    user_ip: Ipv4Addr,
    user_pubkey: Pubkey,
    user_tunnel_block: Pubkey,
    multicast_publisher_block: Pubkey,
    tunnel_ids: Pubkey,
    dz_prefix_block: Pubkey,
}

/// GlobalState/Config, Location, Exchange, Contributor, an Activated Device, five Activated
/// MulticastGroups, and an EdgeSeat access pass with no feeds yet.
async fn setup(client_ip: [u8; 4]) -> Fixture {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (user_tunnel_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);

    let (location_pubkey, exchange_pubkey, contributor_pubkey) = setup_device_prerequisites(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);
    let (tunnel_ids, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_block, _, _) =
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
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids, false),
            AccountMeta::new(dz_prefix_block, false),
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

    let mut groups = Vec::new();
    for i in 0..5 {
        let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                code: format!("group{i}"),
                max_bandwidth: 1000,
                owner: payer.pubkey(),
                use_onchain_allocation: true,
                bond_ref: Pubkey::default(),
            }),
            vec![
                AccountMeta::new(mgroup_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
                AccountMeta::new(
                    get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                    false,
                ),
            ],
            &payer,
        )
        .await;
        groups.push(mgroup_pubkey);
    }

    // EdgeSeat passes are issued at the dynamic (0.0.0.0) PDA so one pass serves every machine the
    // buyer connects, which is what lets two users share a feed's seats below.
    let user_ip: Ipv4Addr = client_ip.into();
    let (accesspass_pubkey, _) =
        get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer.pubkey());
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::EdgeSeat(vec![]),
            client_ip: Ipv4Addr::UNSPECIFIED,
            last_access_epoch: 9999,
            allow_multiple_ip: true,
            max_unicast_users: 1,
            max_multicast_users: 4,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);

    Fixture {
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        exchange_pubkey,
        contributor_pubkey,
        device_pubkey,
        accesspass_pubkey,
        groups,
        user_ip,
        user_pubkey,
        user_tunnel_block,
        multicast_publisher_block,
        tunnel_ids,
        dz_prefix_block,
    }
}

/// Adds `count` physical interfaces to the fixture device, so instructions that load the
/// Device pay for deserializing a realistically sized interface list. Sent with the
/// compute-budget prelude, as the SDK sends CreateDeviceInterface.
async fn add_interfaces(fixture: &mut Fixture, count: usize) {
    let (device_tunnel_block, _, _) =
        get_resource_extension_pda(&fixture.program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids, _, _) =
        get_resource_extension_pda(&fixture.program_id, ResourceType::SegmentRoutingIds);
    for i in 0..count {
        let create = DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: format!("Ethernet{}/{}.{}", i / 8 + 1, i % 8 + 1, 1000 + i),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 10_000_000_000,
            ip_net: None,
            cir: 0,
            mtu: 9000,
            routing_mode: RoutingMode::Static,
            vlan_id: 1000 + i as u16,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
            topology_count: 0,
        });
        let mut ixs = dzi::compute_budget_prelude().to_vec();
        ixs.push(Instruction::new_with_borsh(
            fixture.program_id,
            &create,
            vec![
                AccountMeta::new(fixture.device_pubkey, false),
                AccountMeta::new(fixture.contributor_pubkey, false),
                AccountMeta::new(fixture.globalstate_pubkey, false),
                AccountMeta::new(device_tunnel_block, false),
                AccountMeta::new(segment_routing_ids, false),
                AccountMeta::new(fixture.payer.pubkey(), true),
                AccountMeta::new_readonly(solana_system_interface::program::ID, false),
            ],
        ));
        let mut tx = Transaction::new_with_payer(&ixs, Some(&fixture.payer.pubkey()));
        tx.sign(
            &[&fixture.payer],
            fixture.banks_client.get_latest_blockhash().await.unwrap(),
        );
        fixture.banks_client.process_transaction(tx).await.unwrap();
    }
}

async fn create_feed(
    fixture: &mut Fixture,
    code: &str,
    exchange: Pubkey,
    groups: Vec<Pubkey>,
) -> Pubkey {
    let (feed_pubkey, _) = get_feed_pda(&fixture.program_id, code, &exchange);
    let recent_blockhash = fixture.banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut fixture.banks_client,
        recent_blockhash,
        fixture.program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: code.to_string(),
            name: code.to_string(),
            exchange,
            groups,
            ..Default::default()
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(fixture.globalstate_pubkey, false),
        ],
        &fixture.payer,
    )
    .await;
    feed_pubkey
}

/// Activated multicast groups beyond the five the fixture creates.
async fn create_groups(fixture: &mut Fixture, n: usize) -> Vec<Pubkey> {
    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    let mut groups = Vec::with_capacity(n);
    for i in 0..n {
        let gs = get_globalstate(&mut fixture.banks_client, fixture.globalstate_pubkey).await;
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&fixture.program_id, gs.account_index + 1);
        execute_transaction(
            &mut fixture.banks_client,
            recent_blockhash,
            fixture.program_id,
            DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                code: format!("extra{i}"),
                max_bandwidth: 1000,
                owner: fixture.payer.pubkey(),
                use_onchain_allocation: true,
                bond_ref: Pubkey::default(),
            }),
            vec![
                AccountMeta::new(mgroup_pubkey, false),
                AccountMeta::new(fixture.globalstate_pubkey, false),
                AccountMeta::new(
                    get_resource_extension_pda(
                        &fixture.program_id,
                        ResourceType::MulticastGroupBlock,
                    )
                    .0,
                    false,
                ),
            ],
            &fixture.payer,
        )
        .await;
        groups.push(mgroup_pubkey);
    }
    groups
}

fn seat(feed_key: Pubkey, max_users: u8) -> FeedSeat {
    FeedSeat {
        feed_key,
        max_users,
        max_future_users: max_users,
        current_users: 0,
        anniversary_day: 1,
        window_end: TEST_WINDOW_END,
        terminates_at: TEST_TERMINATES_AT,
    }
}

async fn set_accesspass_feeds(fixture: &mut Fixture, seats: Vec<FeedSeat>) {
    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    let mut accounts = vec![
        AccountMeta::new(fixture.accesspass_pubkey, false),
        AccountMeta::new(fixture.globalstate_pubkey, false),
    ];
    for s in &seats {
        accounts.push(AccountMeta::new(s.feed_key, false));
    }
    let mut tx = create_transaction(
        fixture.program_id,
        &DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: fixture.payer.pubkey(),
            feeds: seats
                .iter()
                .map(|s| FeedSeatConfig {
                    max_users: s.max_users,
                    max_future_users: s.max_future_users,
                    anniversary_day: s.anniversary_day,
                    window_end: s.window_end,
                    terminates_at: s.terminates_at,
                })
                .collect(),
        }),
        &accounts,
        &fixture.payer,
    );
    tx.try_sign(&[&fixture.payer], recent_blockhash).unwrap();
    fixture.banks_client.process_transaction(tx).await.unwrap();
}

/// Bring the Multicast user into existence on `feed` joined to `group` via CreateSubscribeUser,
/// charging that feed's seat at creation.
async fn create_user_on(fixture: &mut Fixture, feed: Pubkey, group: Pubkey) {
    let ip = fixture.user_ip;
    create_user_at(fixture, ip, feed, group).await
}

/// Same, for an arbitrary client IP — a second machine under the same dynamic pass.
async fn create_user_at(fixture: &mut Fixture, ip: Ipv4Addr, feed: Pubkey, group: Pubkey) {
    try_create_user_at(fixture, ip, feed, group).await.unwrap()
}

async fn try_create_user_at(
    fixture: &mut Fixture,
    ip: Ipv4Addr,
    feed: Pubkey,
    group: Pubkey,
) -> Result<(), BanksClientError> {
    let (user_pubkey, _) = get_user_pda(&fixture.program_id, &ip, UserType::Multicast);
    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    let accounts = vec![
        AccountMeta::new(user_pubkey, false),
        AccountMeta::new(fixture.device_pubkey, false),
        AccountMeta::new(group, false),
        AccountMeta::new(fixture.accesspass_pubkey, false),
        AccountMeta::new(fixture.globalstate_pubkey, false),
        AccountMeta::new(fixture.user_tunnel_block, false),
        AccountMeta::new(fixture.multicast_publisher_block, false),
        AccountMeta::new(fixture.tunnel_ids, false),
        AccountMeta::new(fixture.dz_prefix_block, false),
        AccountMeta::new_readonly(feed, false),
    ];
    let mut tx = create_transaction_with_extra_accounts(
        fixture.program_id,
        &DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: ip,
            publisher: false,
            subscriber: true,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            owner: Pubkey::default(),
            ip_proof: None,
            extra_group_count: 0,
        }),
        &accounts,
        &fixture.payer,
        &[],
    );
    tx.try_sign(&[&fixture.payer], recent_blockhash).unwrap();
    fixture.banks_client.process_transaction(tx).await
}

#[allow(clippy::too_many_arguments)]
async fn try_join_as(
    fixture: &mut Fixture,
    user_pubkey: Pubkey,
    device: Pubkey,
    feeds: &[Pubkey],
    groups: &[Pubkey],
) -> Result<(), BanksClientError> {
    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    let mut accounts = vec![
        AccountMeta::new(fixture.accesspass_pubkey, false),
        AccountMeta::new(user_pubkey, false),
        AccountMeta::new(fixture.globalstate_pubkey, false),
        AccountMeta::new_readonly(device, false),
    ];
    accounts.extend(feeds.iter().map(|k| AccountMeta::new_readonly(*k, false)));
    accounts.extend(groups.iter().map(|k| AccountMeta::new(*k, false)));

    let mut tx = create_transaction_with_extra_accounts(
        fixture.program_id,
        &DoubleZeroInstruction::SubscribeFeed(SubscribeFeedArgs {
            feed_count: feeds.len() as u8,
        }),
        &accounts,
        &fixture.payer,
        &[],
    );
    tx.try_sign(&[&fixture.payer], recent_blockhash).unwrap();
    fixture.banks_client.process_transaction(tx).await
}

async fn join(
    fixture: &mut Fixture,
    feeds: &[Pubkey],
    groups: &[Pubkey],
) -> Result<(), BanksClientError> {
    let (user, device) = (fixture.user_pubkey, fixture.device_pubkey);
    try_join_as(fixture, user, device, feeds, groups).await
}

#[allow(clippy::too_many_arguments)]
async fn try_leave_as(
    fixture: &mut Fixture,
    user_pubkey: Pubkey,
    device: Pubkey,
    targets: &[Pubkey],
    retained: &[Pubkey],
    groups: &[Pubkey],
) -> Result<(), BanksClientError> {
    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    let mut accounts = vec![
        AccountMeta::new(fixture.accesspass_pubkey, false),
        AccountMeta::new(user_pubkey, false),
        AccountMeta::new(fixture.globalstate_pubkey, false),
        AccountMeta::new_readonly(device, false),
    ];
    accounts.extend(
        targets
            .iter()
            .chain(retained)
            .map(|k| AccountMeta::new_readonly(*k, false)),
    );
    accounts.extend(groups.iter().map(|k| AccountMeta::new(*k, false)));

    let mut tx = create_transaction_with_extra_accounts(
        fixture.program_id,
        &DoubleZeroInstruction::UnsubscribeFeed(UnsubscribeFeedArgs {
            feed_count: targets.len() as u8,
            retained_feed_count: retained.len() as u8,
        }),
        &accounts,
        &fixture.payer,
        &[],
    );
    tx.try_sign(&[&fixture.payer], recent_blockhash).unwrap();
    fixture.banks_client.process_transaction(tx).await
}

async fn leave(
    fixture: &mut Fixture,
    targets: &[Pubkey],
    retained: &[Pubkey],
    groups: &[Pubkey],
) -> Result<(), BanksClientError> {
    let (user, device) = (fixture.user_pubkey, fixture.device_pubkey);
    try_leave_as(fixture, user, device, targets, retained, groups).await
}

/// An IBRL user under the same pass, for the user-type gate.
async fn create_ibrl_user(fixture: &mut Fixture, ip: Ipv4Addr) {
    let (user_pubkey, _) = get_user_pda(&fixture.program_id, &ip, UserType::IBRL);
    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    execute_transaction(
        &mut fixture.banks_client,
        recent_blockhash,
        fixture.program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            ip_proof: None,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(fixture.device_pubkey, false),
            AccountMeta::new(fixture.accesspass_pubkey, false),
            AccountMeta::new(fixture.globalstate_pubkey, false),
            AccountMeta::new(fixture.user_tunnel_block, false),
            AccountMeta::new(fixture.multicast_publisher_block, false),
            AccountMeta::new(fixture.tunnel_ids, false),
            AccountMeta::new(fixture.dz_prefix_block, false),
        ],
        &fixture.payer,
    )
    .await;
}

async fn read_pass(fixture: &mut Fixture) -> AccessPass {
    get_account_data(&mut fixture.banks_client, fixture.accesspass_pubkey)
        .await
        .expect("access pass")
        .get_accesspass()
        .unwrap()
}

async fn read_user(fixture: &mut Fixture) -> User {
    get_account_data(&mut fixture.banks_client, fixture.user_pubkey)
        .await
        .expect("user")
        .get_user()
        .unwrap()
}

/// Match the error structurally rather than on its debug text, so a test cannot pass because some
/// other instruction in the transaction failed or because the formatting changed.
fn assert_custom_error(err: &BanksClientError, code: u32) {
    match err {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(actual),
        )) if *actual == code => {}
        other => panic!("expected Custom({code}), got {other:?}"),
    }
}

fn seat_users(pass: &AccessPass, feed: &Pubkey) -> u8 {
    pass.feed_seats()
        .iter()
        .find(|s| &s.feed_key == feed)
        .expect("seat for feed")
        .current_users
}

// A feed's whole group set is joined in one transaction, and the three groups inside it cost a
// single seat. The seat is capped at 1, so a per-group tick would fail as FeedSeatFull.
#[tokio::test]
async fn test_feed_subscription_joins_every_group_for_one_seat() {
    let mut fixture = setup([100, 0, 0, 20]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(
        &mut fixture,
        "feed1",
        exchange,
        vec![groups[0], groups[1], groups[2]],
    )
    .await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;

    create_user_on(&mut fixture, feed, groups[0]).await;
    join(&mut fixture, &[feed], &[groups[1], groups[2]])
        .await
        .unwrap();

    let user = read_user(&mut fixture).await;
    assert_eq!(user.subscribers, vec![groups[0], groups[1], groups[2]]);
    assert_eq!(user.feed_pks, vec![feed]);
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &feed), 1);
}

// A group drawn from a second feed takes that feed's own seat, and leaves the first untouched.
#[tokio::test]
async fn test_second_feed_takes_its_own_seat() {
    let mut fixture = setup([100, 0, 0, 21]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed1 = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    let feed2 = create_feed(&mut fixture, "feed2", exchange, vec![groups[2], groups[3]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed1, 1), seat(feed2, 1)]).await;

    create_user_on(&mut fixture, feed1, groups[0]).await;
    join(&mut fixture, &[feed2], &[groups[2], groups[3]])
        .await
        .unwrap();

    let user = read_user(&mut fixture).await;
    assert_eq!(user.feed_pks, vec![feed1, feed2]);
    let pass = read_pass(&mut fixture).await;
    assert_eq!(seat_users(&pass, &feed1), 1);
    assert_eq!(seat_users(&pass, &feed2), 1);
    assert_eq!(pass.public_multicast_user_count, 1);
}

// Both feeds' groups join in a single transaction, taking one seat each.
#[tokio::test]
async fn test_two_feeds_in_one_transaction() {
    let mut fixture = setup([100, 0, 0, 22]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed1 = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    let feed2 = create_feed(&mut fixture, "feed2", exchange, vec![groups[2], groups[3]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed1, 1), seat(feed2, 1)]).await;

    create_user_on(&mut fixture, feed1, groups[0]).await;
    join(
        &mut fixture,
        &[feed1, feed2],
        &[groups[1], groups[2], groups[3]],
    )
    .await
    .unwrap();

    let user = read_user(&mut fixture).await;
    assert_eq!(
        user.subscribers,
        vec![groups[0], groups[1], groups[2], groups[3]]
    );
    let pass = read_pass(&mut fixture).await;
    assert_eq!(seat_users(&pass, &feed1), 1);
    assert_eq!(seat_users(&pass, &feed2), 1);
}

// Dropping the user's last group in a feed releases that feed's seat.
#[tokio::test]
async fn test_removal_releases_the_seat_on_the_last_group() {
    let mut fixture = setup([100, 0, 0, 23]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;

    create_user_on(&mut fixture, feed, groups[0]).await;
    join(&mut fixture, &[feed], &[groups[1]]).await.unwrap();
    leave(&mut fixture, &[feed], &[], &[groups[0], groups[1]])
        .await
        .unwrap();

    let user = read_user(&mut fixture).await;
    assert!(user.subscribers.is_empty());
    assert!(user.feed_pks.is_empty());
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &feed), 0);
}

// Leaving one feed must not drop a group a retained feed still carries. Alice keeps feed1, which
// also sells g0, so leaving feed2 costs her g1 only and leaves feed1's seat charged.
#[tokio::test]
async fn test_leaving_a_feed_keeps_a_group_a_retained_feed_covers() {
    let mut fixture = setup([100, 0, 0, 24]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed1 = create_feed(&mut fixture, "feed1", exchange, vec![groups[0]]).await;
    let feed2 = create_feed(&mut fixture, "feed2", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed1, 1), seat(feed2, 1)]).await;

    create_user_on(&mut fixture, feed1, groups[0]).await;
    join(&mut fixture, &[feed2], &[groups[1]]).await.unwrap();

    // groups[0] is covered by feed1, so only groups[1] departs with feed2.
    leave(&mut fixture, &[feed2], &[feed1], &[groups[1]])
        .await
        .unwrap();

    let user = read_user(&mut fixture).await;
    assert_eq!(user.subscribers, vec![groups[0]]);
    assert_eq!(user.feed_pks, vec![feed1]);
    let pass = read_pass(&mut fixture).await;
    assert_eq!(seat_users(&pass, &feed1), 1);
    assert_eq!(seat_users(&pass, &feed2), 0);
}

// A leave that omits a feed the user still holds is rejected rather than stranding its seat.
#[tokio::test]
async fn test_leave_omitting_a_held_feed_rejected() {
    let mut fixture = setup([100, 0, 0, 32]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed1 = create_feed(&mut fixture, "feed1", exchange, vec![groups[0]]).await;
    let feed2 = create_feed(&mut fixture, "feed2", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed1, 1), seat(feed2, 1)]).await;

    create_user_on(&mut fixture, feed1, groups[0]).await;
    join(&mut fixture, &[feed2], &[groups[1]]).await.unwrap();

    let err = leave(&mut fixture, &[feed2], &[], &[groups[1]])
        .await
        .unwrap_err();
    assert_custom_error(&err, 92);
}

// The group list must be exactly what the target feeds change, so a stale client cannot half-apply.
#[tokio::test]
async fn test_group_list_not_matching_the_feeds_rejected() {
    let mut fixture = setup([100, 0, 0, 25]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_user_on(&mut fixture, feed, groups[0]).await;

    // Same count as the real change set (groups[1]), but the wrong group.
    let err = join(&mut fixture, &[feed], &[groups[4]]).await.unwrap_err();
    assert_custom_error(&err, 65);
}

// A feed that is not provisioned on the pass is rejected: FeedNotOnAccessPass (93).
#[tokio::test]
async fn test_feed_not_on_the_pass_rejected() {
    let mut fixture = setup([100, 0, 0, 26]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    let unprovisioned = create_feed(&mut fixture, "feed2", exchange, vec![groups[2]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_user_on(&mut fixture, feed, groups[0]).await;

    let err = join(&mut fixture, &[unprovisioned], &[groups[2]])
        .await
        .unwrap_err();
    assert_custom_error(&err, 93);
}

// A feed serving a different metro than the user's device is rejected: MetroMismatch (91).
#[tokio::test]
async fn test_feed_serving_another_metro_rejected() {
    let mut fixture = setup([100, 0, 0, 27]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    // A feed keyed to an exchange the device does not sit in.
    let other_metro =
        create_feed(&mut fixture, "feed2", Pubkey::new_unique(), vec![groups[2]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1), seat(other_metro, 1)]).await;
    create_user_on(&mut fixture, feed, groups[0]).await;

    let err = join(&mut fixture, &[other_metro], &[groups[2]])
        .await
        .unwrap_err();
    assert_custom_error(&err, 91);
}

// A device that is not the user's is rejected: UserDeviceMismatch (102). Without this a caller
// could pass any device and have its exchange satisfy the metro check.
#[tokio::test]
async fn test_foreign_device_rejected() {
    let mut fixture = setup([100, 0, 0, 28]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_user_on(&mut fixture, feed, groups[0]).await;

    let (foreign, user) = (fixture.globalstate_pubkey, fixture.user_pubkey);
    let err = try_join_as(&mut fixture, user, foreign, &[feed], &[groups[1]])
        .await
        .unwrap_err();
    assert_custom_error(&err, 102);
}

// Two machines share the pass. A feed sold for one user admits the first and rejects the second
// with FeedSeatFull (95) — the cap is per feed, and this is the boundary a buyer actually hits.
#[tokio::test]
async fn test_seat_cap_rejects_a_second_machine() {
    let mut fixture = setup([100, 0, 0, 29]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    // `entry` has room for both machines and only exists so each user account can be created; until
    // CreateUser goes naked (#4110) creating a user under an EdgeSeat pass always takes a seat.
    // `scarce` is the feed under test, sold for a single user.
    let entry = create_feed(&mut fixture, "entry", exchange, vec![groups[0]]).await;
    let scarce = create_feed(&mut fixture, "scarce", exchange, vec![groups[2]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(entry, 2), seat(scarce, 1)]).await;

    let second_ip: Ipv4Addr = [100, 0, 0, 60].into();
    let (second_user, _) = get_user_pda(&fixture.program_id, &second_ip, UserType::Multicast);
    create_user_on(&mut fixture, entry, groups[0]).await;
    create_user_at(&mut fixture, second_ip, entry, groups[0]).await;
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &entry), 2);

    // Machine 1 takes the scarce feed's only seat.
    join(&mut fixture, &[scarce], &[groups[2]]).await.unwrap();
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &scarce), 1);

    // Machine 2 is a legitimate user on the same pass, but the feed is sold out.
    let device = fixture.device_pubkey;
    let err = try_join_as(&mut fixture, second_user, device, &[scarce], &[groups[2]])
        .await
        .unwrap_err();
    assert_custom_error(&err, 95);
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &scarce), 1);
}

// The comped path still works on an EdgeSeat pass: a group a foundation member put on the
// subscriber allowlist is joinable through UpdateMulticastGroupRoles, and takes no feed seat.
#[tokio::test]
async fn test_allowlisted_group_joins_without_a_seat() {
    let mut fixture = setup([100, 0, 0, 30]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_user_on(&mut fixture, feed, groups[0]).await;

    // Foundation comps groups[4], which no feed carries.
    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    execute_transaction(
        &mut fixture.banks_client,
        recent_blockhash,
        fixture.program_id,
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
            doublezero_serviceability::processors::multicastgroup::allowlist::subscriber::add::AddMulticastGroupSubAllowlistArgs {
                client_ip: fixture.user_ip,
                user_payer: fixture.payer.pubkey(),
            },
        ),
        vec![
            AccountMeta::new(groups[4], false),
            AccountMeta::new(fixture.accesspass_pubkey, false),
            AccountMeta::new(fixture.globalstate_pubkey, false),
            AccountMeta::new(fixture.payer.pubkey(), false),
        ],
        &fixture.payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    let mut tx = create_transaction_with_extra_accounts(
        fixture.program_id,
        &DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: fixture.user_ip,
            publisher: false,
            subscriber: true,
            use_onchain_allocation: true,
            extra_group_count: 0,
        }),
        &vec![
            AccountMeta::new(groups[4], false),
            AccountMeta::new(fixture.accesspass_pubkey, false),
            AccountMeta::new(fixture.user_pubkey, false),
            AccountMeta::new(fixture.globalstate_pubkey, false),
            AccountMeta::new(fixture.multicast_publisher_block, false),
        ],
        &fixture.payer,
        &[],
    );
    tx.try_sign(&[&fixture.payer], recent_blockhash).unwrap();
    fixture.banks_client.process_transaction(tx).await.unwrap();

    let user = read_user(&mut fixture).await;
    assert_eq!(user.subscribers, vec![groups[0], groups[4]]);
    // The comped group belongs to no feed, so it consumed nothing.
    assert_eq!(user.feed_pks, vec![feed]);
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &feed), 1);
}

// The hole this PR closes: an EdgeSeat holder may no longer reach a group through
// UpdateMulticastGroupRoles just because a feed carries it. That path is now allowlist-only, so a
// purchased group must go through UpdateFeedSubscription and charge its seat. NotAllowed (8).
#[tokio::test]
async fn test_feed_group_not_joinable_through_the_roles_instruction() {
    let mut fixture = setup([100, 0, 0, 31]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_user_on(&mut fixture, feed, groups[0]).await;

    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    let mut tx = create_transaction_with_extra_accounts(
        fixture.program_id,
        &DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: fixture.user_ip,
            publisher: false,
            subscriber: true,
            use_onchain_allocation: true,
            extra_group_count: 0,
        }),
        &vec![
            AccountMeta::new(groups[1], false),
            AccountMeta::new(fixture.accesspass_pubkey, false),
            AccountMeta::new(fixture.user_pubkey, false),
            AccountMeta::new(fixture.globalstate_pubkey, false),
            AccountMeta::new(fixture.multicast_publisher_block, false),
        ],
        &fixture.payer,
        &[],
    );
    tx.try_sign(&[&fixture.payer], recent_blockhash).unwrap();
    let err = fixture
        .banks_client
        .process_transaction(tx)
        .await
        .unwrap_err();
    assert_custom_error(&err, 8);
}

// This instruction is EdgeSeat-only: a Prepaid pass carries no feeds and must use the allowlist path.
#[tokio::test]
async fn test_non_edgeseat_pass_rejected() {
    let mut fixture = setup([100, 0, 0, 33]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_user_on(&mut fixture, feed, groups[0]).await;

    // Downgrade the pass to Prepaid, keeping the same PDA and user.
    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    execute_transaction(
        &mut fixture.banks_client,
        recent_blockhash,
        fixture.program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            last_access_epoch: 9999,
            allow_multiple_ip: true,
            max_unicast_users: 1,
            max_multicast_users: 4,
        }),
        vec![
            AccountMeta::new(fixture.accesspass_pubkey, false),
            AccountMeta::new(fixture.globalstate_pubkey, false),
            AccountMeta::new(fixture.payer.pubkey(), false),
        ],
        &fixture.payer,
    )
    .await;

    let err = join(&mut fixture, &[feed], &[groups[1]]).await.unwrap_err();
    assert_custom_error(&err, 101);
}

// Only a Multicast user occupies a feed seat, so no other user type may hold a feed subscription.
#[tokio::test]
async fn test_non_multicast_user_rejected() {
    let mut fixture = setup([100, 0, 0, 34]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 2)]).await;
    create_user_on(&mut fixture, feed, groups[0]).await;

    // An IBRL user at another IP under the same dynamic pass.
    let ibrl_ip: Ipv4Addr = [100, 0, 0, 70].into();
    let (ibrl_user, _) = get_user_pda(&fixture.program_id, &ibrl_ip, UserType::IBRL);
    create_ibrl_user(&mut fixture, ibrl_ip).await;

    let device = fixture.device_pubkey;
    let err = try_join_as(&mut fixture, ibrl_user, device, &[feed], &[groups[1]])
        .await
        .unwrap_err();
    assert_custom_error(&err, 104);
}

// feed_count == 0 leaves nothing to derive the group set from.
#[tokio::test]
async fn test_zero_feed_count_rejected() {
    let mut fixture = setup([100, 0, 0, 35]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_user_on(&mut fixture, feed, groups[0]).await;

    let err = join(&mut fixture, &[], &[groups[1]]).await.unwrap_err();
    assert_custom_error(&err, 65);
}

// The same feed twice would double-count its seat.
#[tokio::test]
async fn test_duplicate_feed_rejected() {
    let mut fixture = setup([100, 0, 0, 36]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_user_on(&mut fixture, feed, groups[0]).await;

    let err = join(&mut fixture, &[feed, feed], &[groups[1]])
        .await
        .unwrap_err();
    assert_custom_error(&err, 65);
}

// Naming the departing feed as retained would make every one of its groups look covered, so nothing
// is unsubscribed while the seat is still released.
#[tokio::test]
async fn test_target_passed_as_retained_rejected() {
    let mut fixture = setup([100, 0, 0, 38]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_user_on(&mut fixture, feed, groups[0]).await;
    join(&mut fixture, &[feed], &[groups[1]]).await.unwrap();

    let err = leave(&mut fixture, &[feed], &[feed], &[])
        .await
        .unwrap_err();
    assert_custom_error(&err, 65);

    // The seat and both subscriptions survive the rejected call.
    let user = read_user(&mut fixture).await;
    assert_eq!(user.subscribers, vec![groups[0], groups[1]]);
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &feed), 1);
}

// A feed the user never held cannot stand in as retained: overlapping groups would survive the leave
// while the target's seat is released.
#[tokio::test]
async fn test_unheld_feed_as_retained_rejected() {
    let mut fixture = setup([100, 0, 0, 39]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let held = create_feed(&mut fixture, "held", exchange, vec![groups[0], groups[1]]).await;
    let unheld = create_feed(&mut fixture, "unheld", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(held, 1), seat(unheld, 1)]).await;
    create_user_on(&mut fixture, held, groups[0]).await;
    join(&mut fixture, &[held], &[groups[1]]).await.unwrap();

    let err = leave(&mut fixture, &[held], &[unheld], &[])
        .await
        .unwrap_err();
    assert_custom_error(&err, 65);

    let user = read_user(&mut fixture).await;
    assert_eq!(user.subscribers, vec![groups[0], groups[1]]);
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &held), 1);
}

// The pass admin drops a feed the user still holds (SetAccessPassFeeds never refuses that). The
// stale feed has no seat left to strand, so it no longer has to be named: the user can still leave
// their other feeds, and the stale entry is pruned along the way.
#[tokio::test]
async fn test_leave_succeeds_after_the_pass_drops_a_held_feed() {
    let mut fixture = setup([100, 0, 0, 41]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed1 = create_feed(&mut fixture, "feed1", exchange, vec![groups[0]]).await;
    let feed2 = create_feed(&mut fixture, "feed2", exchange, vec![groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed1, 1), seat(feed2, 1)]).await;
    create_user_on(&mut fixture, feed1, groups[0]).await;
    join(&mut fixture, &[feed2], &[groups[1]]).await.unwrap();

    set_accesspass_feeds(&mut fixture, vec![seat(feed1, 1)]).await;

    leave(&mut fixture, &[feed1], &[], &[groups[0]])
        .await
        .unwrap();

    let user = read_user(&mut fixture).await;
    assert!(user.feed_pks.is_empty());
    // The stale feed's group stays subscribed until reconciled (or feed2 is named as a target).
    assert_eq!(user.subscribers, vec![groups[1]]);
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &feed1), 0);
}

// A stale feed named as a target unsubscribes its groups, still honoring retained coverage, and
// touches no seat.
#[tokio::test]
async fn test_stale_feed_named_as_target_cleans_up() {
    let mut fixture = setup([100, 0, 0, 42]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed1 = create_feed(&mut fixture, "feed1", exchange, vec![groups[0]]).await;
    let feed2 = create_feed(&mut fixture, "feed2", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed1, 1), seat(feed2, 1)]).await;
    create_user_on(&mut fixture, feed1, groups[0]).await;
    join(&mut fixture, &[feed2], &[groups[1]]).await.unwrap();

    set_accesspass_feeds(&mut fixture, vec![seat(feed1, 1)]).await;

    // groups[0] stays covered by retained feed1; only groups[1] departs with the stale feed2.
    leave(&mut fixture, &[feed2], &[feed1], &[groups[1]])
        .await
        .unwrap();

    let user = read_user(&mut fixture).await;
    assert_eq!(user.subscribers, vec![groups[0]]);
    assert_eq!(user.feed_pks, vec![feed1]);
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &feed1), 1);
}

// A stale feed carries no entitlement, so it cannot stand in as retained coverage.
#[tokio::test]
async fn test_stale_feed_as_retained_rejected() {
    let mut fixture = setup([100, 0, 0, 43]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed1 = create_feed(&mut fixture, "feed1", exchange, vec![groups[0]]).await;
    let feed2 = create_feed(&mut fixture, "feed2", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed1, 1), seat(feed2, 1)]).await;
    create_user_on(&mut fixture, feed1, groups[0]).await;
    join(&mut fixture, &[feed2], &[groups[1]]).await.unwrap();

    set_accesspass_feeds(&mut fixture, vec![seat(feed2, 1)]).await;

    let err = leave(&mut fixture, &[feed2], &[feed1], &[groups[1]])
        .await
        .unwrap_err();
    assert_custom_error(&err, 93);
}

// MAX_FEED_GROUPS is bounded by transaction capacity: a feed at the cap joins in one transaction.
#[tokio::test]
async fn test_feed_at_max_groups_joins_in_one_transaction() {
    let mut fixture = setup([100, 0, 0, 44]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    // `entry` only exists so the user account can be created (see create_user_on).
    let entry = create_feed(&mut fixture, "entry", exchange, vec![groups[0]]).await;
    let mut big_groups = groups[1..5].to_vec();
    let extra = create_groups(&mut fixture, MAX_FEED_GROUPS - big_groups.len()).await;
    big_groups.extend(extra);
    let big = create_feed(&mut fixture, "big", exchange, big_groups.clone()).await;
    set_accesspass_feeds(&mut fixture, vec![seat(entry, 1), seat(big, 1)]).await;
    create_user_on(&mut fixture, entry, groups[0]).await;

    join(&mut fixture, &[big], &big_groups).await.unwrap();

    let user = read_user(&mut fixture).await;
    assert_eq!(user.subscribers.len(), 1 + MAX_FEED_GROUPS);
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &big), 1);
}

// A join that would take the user past MAX_USER_FEEDS is refused: UserFeedLimitExceeded (103).
#[tokio::test]
async fn test_join_past_the_user_feed_cap_rejected() {
    let mut fixture = setup([100, 0, 0, 45]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    // Every feed carries the same group, so each join costs its seat and adds no groups.
    let mut feeds = Vec::new();
    for i in 0..=MAX_USER_FEEDS {
        feeds.push(create_feed(&mut fixture, &format!("feed{i}"), exchange, vec![groups[0]]).await);
    }
    let seats = feeds.iter().map(|k| seat(*k, 1)).collect();
    set_accesspass_feeds(&mut fixture, seats).await;
    create_user_on(&mut fixture, feeds[0], groups[0]).await;

    for feed in &feeds[1..MAX_USER_FEEDS] {
        join(&mut fixture, &[*feed], &[]).await.unwrap();
    }
    assert_eq!(read_user(&mut fixture).await.feed_pks.len(), MAX_USER_FEEDS);

    let err = join(&mut fixture, &[feeds[MAX_USER_FEEDS]], &[])
        .await
        .unwrap_err();
    assert_custom_error(&err, 103);
}

// A naked CreateUser makes a bare multicast user (no feed, no group), and one
// SubscribeFeed then joins a whole feed and charges its seat.
#[tokio::test]
async fn test_naked_create_then_subscribe_feed() {
    let mut fixture = setup([100, 0, 0, 46]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0], groups[1]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;

    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    execute_transaction(
        &mut fixture.banks_client,
        recent_blockhash,
        fixture.program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: fixture.user_ip,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            ip_proof: None,
        }),
        vec![
            AccountMeta::new(fixture.user_pubkey, false),
            AccountMeta::new(fixture.device_pubkey, false),
            AccountMeta::new(fixture.accesspass_pubkey, false),
            AccountMeta::new(fixture.globalstate_pubkey, false),
            AccountMeta::new(fixture.user_tunnel_block, false),
            AccountMeta::new(fixture.multicast_publisher_block, false),
            AccountMeta::new(fixture.tunnel_ids, false),
            AccountMeta::new(fixture.dz_prefix_block, false),
        ],
        &fixture.payer,
    )
    .await;

    let user = read_user(&mut fixture).await;
    assert!(user.subscribers.is_empty());
    assert!(user.feed_pks.is_empty());
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &feed), 0);

    join(&mut fixture, &[feed], &[groups[0], groups[1]])
        .await
        .unwrap();

    let user = read_user(&mut fixture).await;
    assert_eq!(user.subscribers, vec![groups[0], groups[1]]);
    assert_eq!(user.feed_pks, vec![feed]);
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &feed), 1);
}

// Only CreateUser is idempotent: a duplicate CreateSubscribeUser is still rejected, and neither
// ticks a seat nor changes the user.
#[tokio::test]
async fn test_duplicate_create_subscribe_user_rejected() {
    let mut fixture = setup([100, 0, 0, 47]).await;
    let (exchange, groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![groups[0]]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 2)]).await;
    create_user_on(&mut fixture, feed, groups[0]).await;

    let ip = fixture.user_ip;
    let err = try_create_user_at(&mut fixture, ip, feed, groups[0])
        .await
        .unwrap_err();
    match err {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::AccountAlreadyInitialized,
        )) => {}
        other => panic!("expected AccountAlreadyInitialized, got {other:?}"),
    }

    assert_eq!(seat_users(&read_pass(&mut fixture).await, &feed), 1);
    assert_eq!(read_user(&mut fixture).await.subscribers, vec![groups[0]]);
}

fn wire_size(tx: &Transaction) -> usize {
    // Signature count, one 64-byte signature per required signer, then the message.
    1 + 64 * usize::from(tx.message.header.num_required_signatures) + tx.message.serialize().len()
}

// A USER_ADMIN delegate (neither the pass owner nor a foundation member) must send its Permission
// account, so the worst-case single-feed leave (four retained feeds, twenty departing groups) is
// past the packet limit behind the compute-budget prelude. The SDK sends it without the prelude:
// it fits, runs under the default compute limit and heap, and lands in one transaction.
#[tokio::test]
async fn test_user_admin_leaves_a_max_feed_without_prelude() {
    let mut fixture = setup([100, 0, 0, 47]).await;
    // The leave loads the full Device on the default 32 KiB heap. 24 subinterfaces is the
    // largest Device CreateDeviceInterface can grow (the 25th create runs out of heap itself),
    // and above the 14 interfaces the busiest mainnet device carried at epoch 129.
    const INTERFACES: usize = 24;
    add_interfaces(&mut fixture, INTERFACES).await;
    let device = get_device(&mut fixture.banks_client, fixture.device_pubkey)
        .await
        .unwrap();
    assert_eq!(device.interfaces.len(), INTERFACES);
    let (exchange, fixture_groups) = (fixture.exchange_pubkey, fixture.groups.clone());
    // The costliest leave: every held feed carries its own MAX_FEED_GROUPS groups, so the user
    // holds 100 subscriptions, and the pass carries 17 seats. A cheaper shape (kept feeds sharing
    // one group) would hide a leave that runs past the default 200k CU.
    const SEATS: usize = 17;
    let mut kept = Vec::new();
    let mut kept_groups = Vec::new();
    for i in 0..MAX_USER_FEEDS - 1 {
        let groups = create_groups(&mut fixture, MAX_FEED_GROUPS).await;
        kept.push(create_feed(&mut fixture, &format!("kept{i}"), exchange, groups.clone()).await);
        kept_groups.push(groups);
    }
    let mut big_groups = fixture_groups[1..5].to_vec();
    let extra = create_groups(&mut fixture, MAX_FEED_GROUPS - big_groups.len()).await;
    big_groups.extend(extra);
    let big = create_feed(&mut fixture, "big", exchange, big_groups.clone()).await;
    // The user is created on `seed`, which carries only fixture_groups[0]. The user leaves
    // `seed` once it holds the kept feeds.
    let seed = create_feed(&mut fixture, "seed", exchange, vec![fixture_groups[0]]).await;
    let mut fillers = Vec::new();
    for i in 0..SEATS - MAX_USER_FEEDS - 1 {
        fillers.push(
            create_feed(
                &mut fixture,
                &format!("filler{i}"),
                exchange,
                vec![fixture_groups[0]],
            )
            .await,
        );
    }
    let seats = kept
        .iter()
        .chain([&big, &seed])
        .chain(&fillers)
        .map(|k| seat(*k, 1))
        .collect::<Vec<_>>();
    assert_eq!(seats.len(), SEATS);
    set_accesspass_feeds(&mut fixture, seats).await;
    create_user_on(&mut fixture, seed, fixture_groups[0]).await;
    for (feed, groups) in kept.iter().zip(&kept_groups) {
        join(&mut fixture, &[*feed], groups).await.unwrap();
    }
    leave(&mut fixture, &[seed], &kept, &[fixture_groups[0]])
        .await
        .unwrap();
    join(&mut fixture, &[big], &big_groups).await.unwrap();
    assert_eq!(
        read_user(&mut fixture).await.subscribers.len(),
        MAX_USER_FEEDS * MAX_FEED_GROUPS
    );
    assert_eq!(read_user(&mut fixture).await.feed_pks.len(), MAX_USER_FEEDS);

    let delegate = Keypair::new();
    transfer(
        &mut fixture.banks_client,
        &fixture.payer,
        &delegate.pubkey(),
        10_000_000_000,
    )
    .await;
    let mut grant: Vec<Instruction> = dzi::compute_budget_prelude().to_vec();
    grant.push(dzi::permission::create_permission(
        &fixture.program_id,
        &fixture.payer.pubkey(),
        PermissionCreateArgs {
            user_payer: delegate.pubkey(),
            permissions: permission_flags::USER_ADMIN,
        },
    ));
    let mut tx = Transaction::new_with_payer(&grant, Some(&fixture.payer.pubkey()));
    tx.sign(
        &[&fixture.payer],
        wait_for_new_blockhash(&mut fixture.banks_client).await,
    );
    fixture.banks_client.process_transaction(tx).await.unwrap();

    let leave = dzi::multicastgroup::unsubscribe_feed(
        &fixture.program_id,
        &delegate.pubkey(),
        &fixture.accesspass_pubkey,
        &fixture.user_pubkey,
        &fixture.device_pubkey,
        &[big],
        &kept,
        &big_groups,
    );
    let mut with_prelude = dzi::compute_budget_prelude().to_vec();
    with_prelude.push(leave.clone());
    let with_prelude = Transaction::new_with_payer(&with_prelude, Some(&delegate.pubkey()));
    assert!(wire_size(&with_prelude) > 1232);

    let mut tx = Transaction::new_with_payer(&[leave], Some(&delegate.pubkey()));
    assert!(wire_size(&tx) <= 1232, "{} bytes", wire_size(&tx));
    tx.sign(
        &[&delegate],
        wait_for_new_blockhash(&mut fixture.banks_client).await,
    );
    let outcome = fixture
        .banks_client
        .process_transaction_with_metadata(tx)
        .await
        .expect("banks client failure");
    outcome.result.expect("leave must succeed");
    // Without the prelude the leave runs under the default 200k CU limit. The test
    // runtime raises the limit to 1.4M, so check the consumption directly (meaningful
    // under SBF; the native processor reports no CU).
    let consumed = outcome.metadata.expect("metadata").compute_units_consumed;
    assert!(consumed <= 200_000, "{consumed} CU");

    let user = read_user(&mut fixture).await;
    assert_eq!(user.feed_pks, kept);
    let mut held = user.subscribers.clone();
    held.sort();
    let mut want = kept_groups.concat();
    want.sort();
    assert_eq!(held, want);
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &big), 0);
}

async fn make_group_private(fixture: &mut Fixture, group: Pubkey) {
    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    execute_transaction(
        &mut fixture.banks_client,
        recent_blockhash,
        fixture.program_id,
        DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
            private: Some(true),
            max_publishers: Some(10),
            max_subscribers: Some(10),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(group, false),
            AccountMeta::new(fixture.globalstate_pubkey, false),
        ],
        &fixture.payer,
    )
    .await;
}

async fn add_group_to_subscriber_allowlist(fixture: &mut Fixture, group: Pubkey) {
    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    execute_transaction(
        &mut fixture.banks_client,
        recent_blockhash,
        fixture.program_id,
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
            doublezero_serviceability::processors::multicastgroup::allowlist::subscriber::add::AddMulticastGroupSubAllowlistArgs {
                client_ip: fixture.user_ip,
                user_payer: fixture.payer.pubkey(),
            },
        ),
        vec![
            AccountMeta::new(group, false),
            AccountMeta::new(fixture.accesspass_pubkey, false),
            AccountMeta::new(fixture.globalstate_pubkey, false),
            AccountMeta::new(fixture.payer.pubkey(), false),
        ],
        &fixture.payer,
    )
    .await;
}

// This helper sends one CreateSubscribeUser for the fixture host. The first group in `groups` is
// the main group, and the other groups are extra groups.
async fn create_subscribe_user(
    fixture: &mut Fixture,
    user_type: UserType,
    groups: &[Pubkey],
    feed: Option<Pubkey>,
) {
    let (user_pubkey, _) = get_user_pda(&fixture.program_id, &fixture.user_ip, user_type);
    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    let mut accounts = vec![
        AccountMeta::new(user_pubkey, false),
        AccountMeta::new(fixture.device_pubkey, false),
        AccountMeta::new(groups[0], false),
        AccountMeta::new(fixture.accesspass_pubkey, false),
        AccountMeta::new(fixture.globalstate_pubkey, false),
        AccountMeta::new(fixture.user_tunnel_block, false),
        AccountMeta::new(fixture.multicast_publisher_block, false),
        AccountMeta::new(fixture.tunnel_ids, false),
        AccountMeta::new(fixture.dz_prefix_block, false),
    ];
    accounts.extend(groups[1..].iter().map(|k| AccountMeta::new(*k, false)));
    accounts.extend(feed.iter().map(|k| AccountMeta::new_readonly(*k, false)));
    let mut tx = create_transaction_with_extra_accounts(
        fixture.program_id,
        &DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: fixture.user_ip,
            publisher: false,
            subscriber: true,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            owner: Pubkey::default(),
            ip_proof: None,
            extra_group_count: (groups.len() - 1) as u8,
        }),
        &accounts,
        &fixture.payer,
        &[],
    );
    tx.try_sign(&[&fixture.payer], recent_blockhash).unwrap();
    fixture.banks_client.process_transaction(tx).await.unwrap();
}

async fn create_private_group_user(fixture: &mut Fixture, group: Pubkey) {
    create_subscribe_user(fixture, UserType::Multicast, &[group], None).await;
}

#[tokio::test]
async fn test_private_group_user_counts_once_it_joins_a_feed() {
    let mut fixture = setup([100, 0, 0, 50]).await;
    let exchange = fixture.exchange_pubkey;
    let private_group = fixture.groups[0];
    let feed_group = fixture.groups[1];
    make_group_private(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, private_group).await;
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![feed_group]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;

    create_private_group_user(&mut fixture, private_group).await;
    assert_eq!(read_pass(&mut fixture).await.public_multicast_user_count, 0);
    assert!(!read_user(&mut fixture).await.holds_pass_seat);

    join(&mut fixture, &[feed], &[feed_group]).await.unwrap();

    let user = read_user(&mut fixture).await;
    assert_eq!(user.subscribers, vec![private_group, feed_group]);
    assert_eq!(user.feed_pks, vec![feed]);
    assert!(user.holds_pass_seat);
    let pass = read_pass(&mut fixture).await;
    assert_eq!(pass.public_multicast_user_count, 1);
    assert_eq!(seat_users(&pass, &feed), 1);
}

#[tokio::test]
async fn test_private_group_user_feed_join_refused_at_pass_wide_cap() {
    let mut fixture = setup([100, 0, 0, 51]).await;
    let exchange = fixture.exchange_pubkey;
    let private_group = fixture.groups[0];
    let feed_group = fixture.groups[1];
    make_group_private(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, private_group).await;
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![feed_group]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 5)]).await;

    // The fixture pass admits 4 multicast users, and 4 feed users fill it.
    for host in 1..=4 {
        create_user_at(
            &mut fixture,
            Ipv4Addr::new(100, 0, 1, host),
            feed,
            feed_group,
        )
        .await;
    }
    create_private_group_user(&mut fixture, private_group).await;
    assert_eq!(read_pass(&mut fixture).await.public_multicast_user_count, 4);

    let err = join(&mut fixture, &[feed], &[feed_group])
        .await
        .unwrap_err();
    assert_custom_error(
        &err,
        custom_code(DoubleZeroError::AccessPassMaxMulticastUsersExceeded),
    );

    let user = read_user(&mut fixture).await;
    assert!(!user.holds_pass_seat);
    assert!(user.feed_pks.is_empty());
    assert_eq!(seat_users(&read_pass(&mut fixture).await, &feed), 4);
}

async fn leave_private_group(fixture: &mut Fixture, group: Pubkey) {
    set_subscriber_role(fixture, group, false).await;
}

async fn set_subscriber_role(fixture: &mut Fixture, group: Pubkey, subscriber: bool) {
    let recent_blockhash = wait_for_new_blockhash(&mut fixture.banks_client).await;
    let mut tx = create_transaction_with_extra_accounts(
        fixture.program_id,
        &DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: fixture.user_ip,
            publisher: false,
            subscriber,
            use_onchain_allocation: true,
            extra_group_count: 0,
        }),
        &vec![
            AccountMeta::new(group, false),
            AccountMeta::new(fixture.accesspass_pubkey, false),
            AccountMeta::new(fixture.user_pubkey, false),
            AccountMeta::new(fixture.globalstate_pubkey, false),
            AccountMeta::new(fixture.multicast_publisher_block, false),
        ],
        &fixture.payer,
        &[],
    );
    tx.try_sign(&[&fixture.payer], recent_blockhash).unwrap();
    fixture.banks_client.process_transaction(tx).await.unwrap();
}

async fn delete_user(fixture: &mut Fixture) {
    let mut instructions = dzi::compute_budget_prelude().to_vec();
    instructions.push(dzi::user::delete_user(
        &fixture.program_id,
        &fixture.payer.pubkey(),
        &fixture.user_pubkey,
        &fixture.accesspass_pubkey,
        &fixture.device_pubkey,
        1,
        None,
        &fixture.payer.pubkey(),
        AccessPassKind::EdgeSeat,
        UserDeleteArgs {
            dz_prefix_count: 1,
            multicast_publisher_count: 1,
        },
    ));
    process_instructions(&mut fixture.banks_client, &instructions, &fixture.payer)
        .await
        .unwrap();
}

#[tokio::test]
async fn test_delete_feed_user_releases_pass_count() {
    let mut fixture = setup([100, 0, 0, 52]).await;
    let exchange = fixture.exchange_pubkey;
    let feed_group = fixture.groups[0];
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![feed_group]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_user_on(&mut fixture, feed, feed_group).await;
    assert_eq!(read_pass(&mut fixture).await.public_multicast_user_count, 1);

    leave(&mut fixture, &[feed], &[], &[feed_group])
        .await
        .unwrap();
    delete_user(&mut fixture).await;

    let pass = read_pass(&mut fixture).await;
    assert_eq!(pass.public_multicast_user_count, 0);
    assert_eq!(pass.connection_count, 0);
    assert_eq!(seat_users(&pass, &feed), 0);
}

#[tokio::test]
async fn test_delete_private_group_user_keeps_pass_count() {
    let mut fixture = setup([100, 0, 0, 53]).await;
    let exchange = fixture.exchange_pubkey;
    let private_group = fixture.groups[0];
    let feed_group = fixture.groups[1];
    make_group_private(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, private_group).await;
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![feed_group]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;

    // A feed user on another host holds the only raised count.
    create_user_at(&mut fixture, Ipv4Addr::new(100, 0, 1, 1), feed, feed_group).await;
    create_private_group_user(&mut fixture, private_group).await;
    assert_eq!(read_pass(&mut fixture).await.public_multicast_user_count, 1);

    leave_private_group(&mut fixture, private_group).await;
    delete_user(&mut fixture).await;

    let pass = read_pass(&mut fixture).await;
    assert_eq!(pass.public_multicast_user_count, 1);
    assert_eq!(pass.connection_count, 1);
}

#[tokio::test]
async fn test_delete_private_group_user_that_joined_a_feed_releases_pass_count() {
    let mut fixture = setup([100, 0, 0, 54]).await;
    let exchange = fixture.exchange_pubkey;
    let private_group = fixture.groups[0];
    let feed_group = fixture.groups[1];
    make_group_private(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, private_group).await;
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![feed_group]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;

    create_private_group_user(&mut fixture, private_group).await;
    join(&mut fixture, &[feed], &[feed_group]).await.unwrap();
    assert_eq!(read_pass(&mut fixture).await.public_multicast_user_count, 1);

    leave(&mut fixture, &[feed], &[], &[feed_group])
        .await
        .unwrap();
    leave_private_group(&mut fixture, private_group).await;
    delete_user(&mut fixture).await;

    let pass = read_pass(&mut fixture).await;
    assert_eq!(pass.public_multicast_user_count, 0);
    assert_eq!(pass.connection_count, 0);
    assert_eq!(seat_users(&pass, &feed), 0);
}

#[tokio::test]
async fn test_private_group_user_counts_once_it_joins_an_allowlisted_public_group() {
    let mut fixture = setup([100, 0, 0, 55]).await;
    let private_group = fixture.groups[0];
    let public_group = fixture.groups[1];
    make_group_private(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, public_group).await;

    create_private_group_user(&mut fixture, private_group).await;
    assert_eq!(read_pass(&mut fixture).await.public_multicast_user_count, 0);

    set_subscriber_role(&mut fixture, public_group, true).await;

    let user = read_user(&mut fixture).await;
    assert_eq!(user.subscribers, vec![private_group, public_group]);
    assert!(user.holds_pass_seat);
    assert_eq!(read_pass(&mut fixture).await.public_multicast_user_count, 1);

    set_subscriber_role(&mut fixture, public_group, false).await;
    leave_private_group(&mut fixture, private_group).await;
    delete_user(&mut fixture).await;
    assert_eq!(read_pass(&mut fixture).await.public_multicast_user_count, 0);
}

#[tokio::test]
async fn test_private_group_user_joining_another_private_group_does_not_count() {
    let mut fixture = setup([100, 0, 0, 56]).await;
    let private_group = fixture.groups[0];
    let other_private_group = fixture.groups[1];
    make_group_private(&mut fixture, private_group).await;
    make_group_private(&mut fixture, other_private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, other_private_group).await;

    create_private_group_user(&mut fixture, private_group).await;
    set_subscriber_role(&mut fixture, other_private_group, true).await;

    assert!(!read_user(&mut fixture).await.holds_pass_seat);
    assert_eq!(read_pass(&mut fixture).await.public_multicast_user_count, 0);
}

#[tokio::test]
async fn test_ibrl_user_on_a_private_group_counts_against_the_pass() {
    let mut fixture = setup([100, 0, 0, 57]).await;
    let private_group = fixture.groups[0];
    make_group_private(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, private_group).await;

    create_subscribe_user(&mut fixture, UserType::IBRL, &[private_group], None).await;

    let (ibrl_user, _) = get_user_pda(&fixture.program_id, &fixture.user_ip, UserType::IBRL);
    let user = get_account_data(&mut fixture.banks_client, ibrl_user)
        .await
        .expect("user")
        .get_user()
        .unwrap();
    assert!(user.holds_pass_seat);
    let pass = read_pass(&mut fixture).await;
    assert_eq!(pass.unicast_user_count, 1);
    assert_eq!(pass.public_multicast_user_count, 0);
}

#[tokio::test]
async fn test_batch_with_a_private_group_and_a_feed_group_counts_once() {
    let mut fixture = setup([100, 0, 0, 58]).await;
    let exchange = fixture.exchange_pubkey;
    let private_group = fixture.groups[0];
    let feed_group = fixture.groups[1];
    make_group_private(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, private_group).await;
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![feed_group]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;

    create_subscribe_user(
        &mut fixture,
        UserType::Multicast,
        &[private_group, feed_group],
        Some(feed),
    )
    .await;

    let user = read_user(&mut fixture).await;
    assert_eq!(user.subscribers, vec![private_group, feed_group]);
    assert_eq!(user.feed_pks, vec![feed]);
    assert!(user.holds_pass_seat);
    let pass = read_pass(&mut fixture).await;
    assert_eq!(pass.public_multicast_user_count, 1);
    assert_eq!(seat_users(&pass, &feed), 1);
}

#[tokio::test]
async fn test_private_group_user_gives_back_its_pass_seat_when_it_leaves_its_last_feed() {
    let mut fixture = setup([100, 0, 0, 59]).await;
    let exchange = fixture.exchange_pubkey;
    let private_group = fixture.groups[0];
    let feed_group = fixture.groups[1];
    make_group_private(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, private_group).await;
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![feed_group]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_private_group_user(&mut fixture, private_group).await;
    join(&mut fixture, &[feed], &[feed_group]).await.unwrap();
    assert_eq!(read_pass(&mut fixture).await.public_multicast_user_count, 1);

    leave(&mut fixture, &[feed], &[], &[feed_group, private_group])
        .await
        .unwrap();

    let user = read_user(&mut fixture).await;
    assert_eq!(user.subscribers, vec![private_group]);
    assert!(user.feed_pks.is_empty());
    assert!(!user.holds_pass_seat);
    let pass = read_pass(&mut fixture).await;
    assert_eq!(pass.public_multicast_user_count, 0);
    assert_eq!(seat_users(&pass, &feed), 0);
}

#[tokio::test]
async fn test_user_on_a_public_group_keeps_its_pass_seat_when_it_leaves_its_last_feed() {
    let mut fixture = setup([100, 0, 0, 60]).await;
    let exchange = fixture.exchange_pubkey;
    let private_group = fixture.groups[0];
    let feed_group = fixture.groups[1];
    let public_group = fixture.groups[2];
    make_group_private(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, public_group).await;
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![feed_group]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_private_group_user(&mut fixture, private_group).await;
    join(&mut fixture, &[feed], &[feed_group]).await.unwrap();
    set_subscriber_role(&mut fixture, public_group, true).await;

    leave(
        &mut fixture,
        &[feed],
        &[],
        &[feed_group, private_group, public_group],
    )
    .await
    .unwrap();

    assert!(read_user(&mut fixture).await.holds_pass_seat);
    assert_eq!(read_pass(&mut fixture).await.public_multicast_user_count, 1);
}

#[tokio::test]
async fn test_kept_groups_must_match_the_groups_the_user_keeps() {
    let mut fixture = setup([100, 0, 0, 61]).await;
    let exchange = fixture.exchange_pubkey;
    let private_group = fixture.groups[0];
    let feed_group = fixture.groups[1];
    make_group_private(&mut fixture, private_group).await;
    add_group_to_subscriber_allowlist(&mut fixture, private_group).await;
    let feed = create_feed(&mut fixture, "feed1", exchange, vec![feed_group]).await;
    set_accesspass_feeds(&mut fixture, vec![seat(feed, 1)]).await;
    create_private_group_user(&mut fixture, private_group).await;
    join(&mut fixture, &[feed], &[feed_group]).await.unwrap();

    let other_group = fixture.groups[3];
    let err = leave(&mut fixture, &[feed], &[], &[feed_group, other_group])
        .await
        .unwrap_err();
    assert_custom_error(&err, custom_code(DoubleZeroError::InvalidArgument));
    assert!(read_user(&mut fixture).await.holds_pass_seat);
}
