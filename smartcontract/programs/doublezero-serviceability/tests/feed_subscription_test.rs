//! `UpdateFeedSubscription` (variant 117) — joining and leaving whole feeds on an EdgeSeat pass.
//!
//! The property under test throughout is that a seat is held per *feed*, not per group: three
//! groups inside one feed cost one seat, a second feed costs a second, and a seat is released only
//! when the user's last group in that feed goes away.

use doublezero_serviceability::{
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
        device::{create::DeviceCreateArgs, update::DeviceUpdateArgs},
        feed::create::{FeedCreateArgs, MAX_FEED_GROUPS},
        multicastgroup::{
            create::MulticastGroupCreateArgs,
            subscribe::UpdateMulticastGroupRolesArgs,
            subscribe_feed::{SubscribeFeedArgs, MAX_USER_FEEDS},
            unsubscribe_feed::UnsubscribeFeedArgs,
        },
        user::{create::UserCreateArgs, create_subscribe::UserCreateSubscribeArgs},
    },
    resource::ResourceType,
    state::{
        accesspass::{AccessPass, AccessPassType, FeedSeat},
        device::DeviceType,
        user::{User, UserCYOA, UserType},
    },
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signature::Signer,
    transaction::TransactionError,
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

async fn create_feed(f: &mut Fixture, code: &str, exchange: Pubkey, groups: Vec<Pubkey>) -> Pubkey {
    let (feed_pubkey, _) = get_feed_pda(&f.program_id, code, &exchange);
    let recent_blockhash = f.banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut f.banks_client,
        recent_blockhash,
        f.program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: code.to_string(),
            name: code.to_string(),
            exchange,
            groups,
            ..Default::default()
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(f.globalstate_pubkey, false),
        ],
        &f.payer,
    )
    .await;
    feed_pubkey
}

/// Activated multicast groups beyond the five the fixture creates.
async fn create_groups(f: &mut Fixture, n: usize) -> Vec<Pubkey> {
    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    let mut groups = Vec::with_capacity(n);
    for i in 0..n {
        let gs = get_globalstate(&mut f.banks_client, f.globalstate_pubkey).await;
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&f.program_id, gs.account_index + 1);
        execute_transaction(
            &mut f.banks_client,
            recent_blockhash,
            f.program_id,
            DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                code: format!("extra{i}"),
                max_bandwidth: 1000,
                owner: f.payer.pubkey(),
                use_onchain_allocation: true,
            }),
            vec![
                AccountMeta::new(mgroup_pubkey, false),
                AccountMeta::new(f.globalstate_pubkey, false),
                AccountMeta::new(
                    get_resource_extension_pda(&f.program_id, ResourceType::MulticastGroupBlock).0,
                    false,
                ),
            ],
            &f.payer,
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

async fn set_pass_feeds(f: &mut Fixture, seats: Vec<FeedSeat>) {
    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    let mut accounts = vec![
        AccountMeta::new(f.accesspass_pubkey, false),
        AccountMeta::new(f.globalstate_pubkey, false),
    ];
    for s in &seats {
        accounts.push(AccountMeta::new(s.feed_key, false));
    }
    let mut tx = create_transaction(
        f.program_id,
        &DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: f.payer.pubkey(),
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
        &f.payer,
    );
    tx.try_sign(&[&f.payer], recent_blockhash).unwrap();
    f.banks_client.process_transaction(tx).await.unwrap();
}

/// Bring the Multicast user into existence on `feed` joined to `group` via CreateSubscribeUser,
/// charging that feed's seat at creation.
async fn create_user_on(f: &mut Fixture, feed: Pubkey, group: Pubkey) {
    let ip = f.user_ip;
    create_user_at(f, ip, feed, group).await
}

/// Same, for an arbitrary client IP — a second machine under the same dynamic pass.
async fn create_user_at(f: &mut Fixture, ip: Ipv4Addr, feed: Pubkey, group: Pubkey) {
    try_create_user_at(f, ip, feed, group).await.unwrap()
}

async fn try_create_user_at(
    f: &mut Fixture,
    ip: Ipv4Addr,
    feed: Pubkey,
    group: Pubkey,
) -> Result<(), BanksClientError> {
    let (user_pubkey, _) = get_user_pda(&f.program_id, &ip, UserType::Multicast);
    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    let accounts = vec![
        AccountMeta::new(user_pubkey, false),
        AccountMeta::new(f.device_pubkey, false),
        AccountMeta::new(group, false),
        AccountMeta::new(f.accesspass_pubkey, false),
        AccountMeta::new(f.globalstate_pubkey, false),
        AccountMeta::new(f.user_tunnel_block, false),
        AccountMeta::new(f.multicast_publisher_block, false),
        AccountMeta::new(f.tunnel_ids, false),
        AccountMeta::new(f.dz_prefix_block, false),
        AccountMeta::new_readonly(feed, false),
    ];
    let mut tx = create_transaction_with_extra_accounts(
        f.program_id,
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
        &f.payer,
        &[],
    );
    tx.try_sign(&[&f.payer], recent_blockhash).unwrap();
    f.banks_client.process_transaction(tx).await
}

#[allow(clippy::too_many_arguments)]
async fn try_join_as(
    f: &mut Fixture,
    user_pubkey: Pubkey,
    device: Pubkey,
    feeds: &[Pubkey],
    groups: &[Pubkey],
) -> Result<(), BanksClientError> {
    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    let mut accounts = vec![
        AccountMeta::new(f.accesspass_pubkey, false),
        AccountMeta::new(user_pubkey, false),
        AccountMeta::new(f.globalstate_pubkey, false),
        AccountMeta::new_readonly(device, false),
    ];
    accounts.extend(feeds.iter().map(|k| AccountMeta::new_readonly(*k, false)));
    accounts.extend(groups.iter().map(|k| AccountMeta::new(*k, false)));

    let mut tx = create_transaction_with_extra_accounts(
        f.program_id,
        &DoubleZeroInstruction::SubscribeFeed(SubscribeFeedArgs {
            feed_count: feeds.len() as u8,
        }),
        &accounts,
        &f.payer,
        &[],
    );
    tx.try_sign(&[&f.payer], recent_blockhash).unwrap();
    f.banks_client.process_transaction(tx).await
}

async fn join(
    f: &mut Fixture,
    feeds: &[Pubkey],
    groups: &[Pubkey],
) -> Result<(), BanksClientError> {
    let (user, device) = (f.user_pubkey, f.device_pubkey);
    try_join_as(f, user, device, feeds, groups).await
}

#[allow(clippy::too_many_arguments)]
async fn try_leave_as(
    f: &mut Fixture,
    user_pubkey: Pubkey,
    device: Pubkey,
    targets: &[Pubkey],
    retained: &[Pubkey],
    groups: &[Pubkey],
) -> Result<(), BanksClientError> {
    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    let mut accounts = vec![
        AccountMeta::new(f.accesspass_pubkey, false),
        AccountMeta::new(user_pubkey, false),
        AccountMeta::new(f.globalstate_pubkey, false),
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
        f.program_id,
        &DoubleZeroInstruction::UnsubscribeFeed(UnsubscribeFeedArgs {
            feed_count: targets.len() as u8,
            retained_feed_count: retained.len() as u8,
        }),
        &accounts,
        &f.payer,
        &[],
    );
    tx.try_sign(&[&f.payer], recent_blockhash).unwrap();
    f.banks_client.process_transaction(tx).await
}

async fn leave(
    f: &mut Fixture,
    targets: &[Pubkey],
    retained: &[Pubkey],
    groups: &[Pubkey],
) -> Result<(), BanksClientError> {
    let (user, device) = (f.user_pubkey, f.device_pubkey);
    try_leave_as(f, user, device, targets, retained, groups).await
}

/// An IBRL user under the same pass, for the user-type gate.
async fn create_ibrl_user(f: &mut Fixture, ip: Ipv4Addr) {
    let (user_pubkey, _) = get_user_pda(&f.program_id, &ip, UserType::IBRL);
    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    execute_transaction(
        &mut f.banks_client,
        recent_blockhash,
        f.program_id,
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
            AccountMeta::new(f.device_pubkey, false),
            AccountMeta::new(f.accesspass_pubkey, false),
            AccountMeta::new(f.globalstate_pubkey, false),
            AccountMeta::new(f.user_tunnel_block, false),
            AccountMeta::new(f.multicast_publisher_block, false),
            AccountMeta::new(f.tunnel_ids, false),
            AccountMeta::new(f.dz_prefix_block, false),
        ],
        &f.payer,
    )
    .await;
}

async fn read_pass(f: &mut Fixture) -> AccessPass {
    get_account_data(&mut f.banks_client, f.accesspass_pubkey)
        .await
        .expect("access pass")
        .get_accesspass()
        .unwrap()
}

async fn read_user(f: &mut Fixture) -> User {
    get_account_data(&mut f.banks_client, f.user_pubkey)
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
    let mut f = setup([100, 0, 0, 20]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1], g[2]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1)]).await;

    create_user_on(&mut f, feed, g[0]).await;
    join(&mut f, &[feed], &[g[1], g[2]]).await.unwrap();

    let user = read_user(&mut f).await;
    assert_eq!(user.subscribers, vec![g[0], g[1], g[2]]);
    assert_eq!(user.feed_pks, vec![feed]);
    assert_eq!(seat_users(&read_pass(&mut f).await, &feed), 1);
}

// A group drawn from a second feed takes that feed's own seat, and leaves the first untouched.
#[tokio::test]
async fn test_second_feed_takes_its_own_seat() {
    let mut f = setup([100, 0, 0, 21]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed1 = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    let feed2 = create_feed(&mut f, "feed2", exchange, vec![g[2], g[3]]).await;
    set_pass_feeds(&mut f, vec![seat(feed1, 1), seat(feed2, 1)]).await;

    create_user_on(&mut f, feed1, g[0]).await;
    join(&mut f, &[feed2], &[g[2], g[3]]).await.unwrap();

    let user = read_user(&mut f).await;
    assert_eq!(user.feed_pks, vec![feed1, feed2]);
    let pass = read_pass(&mut f).await;
    assert_eq!(seat_users(&pass, &feed1), 1);
    assert_eq!(seat_users(&pass, &feed2), 1);
}

// Both feeds' groups join in a single transaction, taking one seat each.
#[tokio::test]
async fn test_two_feeds_in_one_transaction() {
    let mut f = setup([100, 0, 0, 22]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed1 = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    let feed2 = create_feed(&mut f, "feed2", exchange, vec![g[2], g[3]]).await;
    set_pass_feeds(&mut f, vec![seat(feed1, 1), seat(feed2, 1)]).await;

    create_user_on(&mut f, feed1, g[0]).await;
    join(&mut f, &[feed1, feed2], &[g[1], g[2], g[3]])
        .await
        .unwrap();

    let user = read_user(&mut f).await;
    assert_eq!(user.subscribers, vec![g[0], g[1], g[2], g[3]]);
    let pass = read_pass(&mut f).await;
    assert_eq!(seat_users(&pass, &feed1), 1);
    assert_eq!(seat_users(&pass, &feed2), 1);
}

// Dropping the user's last group in a feed releases that feed's seat.
#[tokio::test]
async fn test_removal_releases_the_seat_on_the_last_group() {
    let mut f = setup([100, 0, 0, 23]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1)]).await;

    create_user_on(&mut f, feed, g[0]).await;
    join(&mut f, &[feed], &[g[1]]).await.unwrap();
    leave(&mut f, &[feed], &[], &[g[0], g[1]]).await.unwrap();

    let user = read_user(&mut f).await;
    assert!(user.subscribers.is_empty());
    assert!(user.feed_pks.is_empty());
    assert_eq!(seat_users(&read_pass(&mut f).await, &feed), 0);
}

// Leaving one feed must not drop a group a retained feed still carries. Alice keeps feed1, which
// also sells g0, so leaving feed2 costs her g1 only and leaves feed1's seat charged.
#[tokio::test]
async fn test_leaving_a_feed_keeps_a_group_a_retained_feed_covers() {
    let mut f = setup([100, 0, 0, 24]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed1 = create_feed(&mut f, "feed1", exchange, vec![g[0]]).await;
    let feed2 = create_feed(&mut f, "feed2", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed1, 1), seat(feed2, 1)]).await;

    create_user_on(&mut f, feed1, g[0]).await;
    join(&mut f, &[feed2], &[g[1]]).await.unwrap();

    // g[0] is covered by feed1, so only g[1] departs with feed2.
    leave(&mut f, &[feed2], &[feed1], &[g[1]]).await.unwrap();

    let user = read_user(&mut f).await;
    assert_eq!(user.subscribers, vec![g[0]]);
    assert_eq!(user.feed_pks, vec![feed1]);
    let pass = read_pass(&mut f).await;
    assert_eq!(seat_users(&pass, &feed1), 1);
    assert_eq!(seat_users(&pass, &feed2), 0);
}

// A leave that omits a feed the user still holds is rejected rather than stranding its seat.
#[tokio::test]
async fn test_leave_omitting_a_held_feed_rejected() {
    let mut f = setup([100, 0, 0, 32]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed1 = create_feed(&mut f, "feed1", exchange, vec![g[0]]).await;
    let feed2 = create_feed(&mut f, "feed2", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed1, 1), seat(feed2, 1)]).await;

    create_user_on(&mut f, feed1, g[0]).await;
    join(&mut f, &[feed2], &[g[1]]).await.unwrap();

    let err = leave(&mut f, &[feed2], &[], &[g[1]]).await.unwrap_err();
    assert_custom_error(&err, 92);
}

// The group list must be exactly what the target feeds change, so a stale client cannot half-apply.
#[tokio::test]
async fn test_group_list_not_matching_the_feeds_rejected() {
    let mut f = setup([100, 0, 0, 25]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1)]).await;
    create_user_on(&mut f, feed, g[0]).await;

    // Same count as the real change set (g[1]), but the wrong group.
    let err = join(&mut f, &[feed], &[g[4]]).await.unwrap_err();
    assert_custom_error(&err, 65);
}

// A feed that is not provisioned on the pass is rejected: FeedNotOnAccessPass (93).
#[tokio::test]
async fn test_feed_not_on_the_pass_rejected() {
    let mut f = setup([100, 0, 0, 26]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    let unprovisioned = create_feed(&mut f, "feed2", exchange, vec![g[2]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1)]).await;
    create_user_on(&mut f, feed, g[0]).await;

    let err = join(&mut f, &[unprovisioned], &[g[2]]).await.unwrap_err();
    assert_custom_error(&err, 93);
}

// A feed serving a different metro than the user's device is rejected: MetroMismatch (91).
#[tokio::test]
async fn test_feed_serving_another_metro_rejected() {
    let mut f = setup([100, 0, 0, 27]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    // A feed keyed to an exchange the device does not sit in.
    let other_metro = create_feed(&mut f, "feed2", Pubkey::new_unique(), vec![g[2]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1), seat(other_metro, 1)]).await;
    create_user_on(&mut f, feed, g[0]).await;

    let err = join(&mut f, &[other_metro], &[g[2]]).await.unwrap_err();
    assert_custom_error(&err, 91);
}

// A device that is not the user's is rejected: UserDeviceMismatch (102). Without this a caller
// could pass any device and have its exchange satisfy the metro check.
#[tokio::test]
async fn test_foreign_device_rejected() {
    let mut f = setup([100, 0, 0, 28]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1)]).await;
    create_user_on(&mut f, feed, g[0]).await;

    let (foreign, user) = (f.globalstate_pubkey, f.user_pubkey);
    let err = try_join_as(&mut f, user, foreign, &[feed], &[g[1]])
        .await
        .unwrap_err();
    assert_custom_error(&err, 102);
}

// Two machines share the pass. A feed sold for one user admits the first and rejects the second
// with FeedSeatFull (95) — the cap is per feed, and this is the boundary a buyer actually hits.
#[tokio::test]
async fn test_seat_cap_rejects_a_second_machine() {
    let mut f = setup([100, 0, 0, 29]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    // `entry` has room for both machines and only exists so each user account can be created; until
    // CreateUser goes naked (#4110) creating a user under an EdgeSeat pass always takes a seat.
    // `scarce` is the feed under test, sold for a single user.
    let entry = create_feed(&mut f, "entry", exchange, vec![g[0]]).await;
    let scarce = create_feed(&mut f, "scarce", exchange, vec![g[2]]).await;
    set_pass_feeds(&mut f, vec![seat(entry, 2), seat(scarce, 1)]).await;

    let second_ip: Ipv4Addr = [100, 0, 0, 60].into();
    let (second_user, _) = get_user_pda(&f.program_id, &second_ip, UserType::Multicast);
    create_user_on(&mut f, entry, g[0]).await;
    create_user_at(&mut f, second_ip, entry, g[0]).await;
    assert_eq!(seat_users(&read_pass(&mut f).await, &entry), 2);

    // Machine 1 takes the scarce feed's only seat.
    join(&mut f, &[scarce], &[g[2]]).await.unwrap();
    assert_eq!(seat_users(&read_pass(&mut f).await, &scarce), 1);

    // Machine 2 is a legitimate user on the same pass, but the feed is sold out.
    let device = f.device_pubkey;
    let err = try_join_as(&mut f, second_user, device, &[scarce], &[g[2]])
        .await
        .unwrap_err();
    assert_custom_error(&err, 95);
    assert_eq!(seat_users(&read_pass(&mut f).await, &scarce), 1);
}

// The comped path still works on an EdgeSeat pass: a group a foundation member put on the
// subscriber allowlist is joinable through UpdateMulticastGroupRoles, and takes no feed seat.
#[tokio::test]
async fn test_allowlisted_group_joins_without_a_seat() {
    let mut f = setup([100, 0, 0, 30]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1)]).await;
    create_user_on(&mut f, feed, g[0]).await;

    // Foundation comps g[4], which no feed carries.
    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    execute_transaction(
        &mut f.banks_client,
        recent_blockhash,
        f.program_id,
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
            doublezero_serviceability::processors::multicastgroup::allowlist::subscriber::add::AddMulticastGroupSubAllowlistArgs {
                client_ip: f.user_ip,
                user_payer: f.payer.pubkey(),
            },
        ),
        vec![
            AccountMeta::new(g[4], false),
            AccountMeta::new(f.accesspass_pubkey, false),
            AccountMeta::new(f.globalstate_pubkey, false),
            AccountMeta::new(f.payer.pubkey(), false),
        ],
        &f.payer,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    let mut tx = create_transaction_with_extra_accounts(
        f.program_id,
        &DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: f.user_ip,
            publisher: false,
            subscriber: true,
            use_onchain_allocation: true,
            extra_group_count: 0,
        }),
        &vec![
            AccountMeta::new(g[4], false),
            AccountMeta::new(f.accesspass_pubkey, false),
            AccountMeta::new(f.user_pubkey, false),
            AccountMeta::new(f.globalstate_pubkey, false),
            AccountMeta::new(f.multicast_publisher_block, false),
        ],
        &f.payer,
        &[],
    );
    tx.try_sign(&[&f.payer], recent_blockhash).unwrap();
    f.banks_client.process_transaction(tx).await.unwrap();

    let user = read_user(&mut f).await;
    assert_eq!(user.subscribers, vec![g[0], g[4]]);
    // The comped group belongs to no feed, so it consumed nothing.
    assert_eq!(user.feed_pks, vec![feed]);
    assert_eq!(seat_users(&read_pass(&mut f).await, &feed), 1);
}

// The hole this PR closes: an EdgeSeat holder may no longer reach a group through
// UpdateMulticastGroupRoles just because a feed carries it. That path is now allowlist-only, so a
// purchased group must go through UpdateFeedSubscription and charge its seat. NotAllowed (8).
#[tokio::test]
async fn test_feed_group_not_joinable_through_the_roles_instruction() {
    let mut f = setup([100, 0, 0, 31]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1)]).await;
    create_user_on(&mut f, feed, g[0]).await;

    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    let mut tx = create_transaction_with_extra_accounts(
        f.program_id,
        &DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: f.user_ip,
            publisher: false,
            subscriber: true,
            use_onchain_allocation: true,
            extra_group_count: 0,
        }),
        &vec![
            AccountMeta::new(g[1], false),
            AccountMeta::new(f.accesspass_pubkey, false),
            AccountMeta::new(f.user_pubkey, false),
            AccountMeta::new(f.globalstate_pubkey, false),
            AccountMeta::new(f.multicast_publisher_block, false),
        ],
        &f.payer,
        &[],
    );
    tx.try_sign(&[&f.payer], recent_blockhash).unwrap();
    let err = f.banks_client.process_transaction(tx).await.unwrap_err();
    assert_custom_error(&err, 8);
}

// This instruction is EdgeSeat-only: a Prepaid pass carries no feeds and must use the allowlist path.
#[tokio::test]
async fn test_non_edgeseat_pass_rejected() {
    let mut f = setup([100, 0, 0, 33]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1)]).await;
    create_user_on(&mut f, feed, g[0]).await;

    // Downgrade the pass to Prepaid, keeping the same PDA and user.
    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    execute_transaction(
        &mut f.banks_client,
        recent_blockhash,
        f.program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            last_access_epoch: 9999,
            allow_multiple_ip: true,
            max_unicast_users: 1,
            max_multicast_users: 4,
        }),
        vec![
            AccountMeta::new(f.accesspass_pubkey, false),
            AccountMeta::new(f.globalstate_pubkey, false),
            AccountMeta::new(f.payer.pubkey(), false),
        ],
        &f.payer,
    )
    .await;

    let err = join(&mut f, &[feed], &[g[1]]).await.unwrap_err();
    assert_custom_error(&err, 101);
}

// Only a Multicast user occupies a feed seat, so no other user type may hold a feed subscription.
#[tokio::test]
async fn test_non_multicast_user_rejected() {
    let mut f = setup([100, 0, 0, 34]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 2)]).await;
    create_user_on(&mut f, feed, g[0]).await;

    // An IBRL user at another IP under the same dynamic pass.
    let ibrl_ip: Ipv4Addr = [100, 0, 0, 70].into();
    let (ibrl_user, _) = get_user_pda(&f.program_id, &ibrl_ip, UserType::IBRL);
    create_ibrl_user(&mut f, ibrl_ip).await;

    let device = f.device_pubkey;
    let err = try_join_as(&mut f, ibrl_user, device, &[feed], &[g[1]])
        .await
        .unwrap_err();
    assert_custom_error(&err, 104);
}

// feed_count == 0 leaves nothing to derive the group set from.
#[tokio::test]
async fn test_zero_feed_count_rejected() {
    let mut f = setup([100, 0, 0, 35]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1)]).await;
    create_user_on(&mut f, feed, g[0]).await;

    let err = join(&mut f, &[], &[g[1]]).await.unwrap_err();
    assert_custom_error(&err, 65);
}

// The same feed twice would double-count its seat.
#[tokio::test]
async fn test_duplicate_feed_rejected() {
    let mut f = setup([100, 0, 0, 36]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1)]).await;
    create_user_on(&mut f, feed, g[0]).await;

    let err = join(&mut f, &[feed, feed], &[g[1]]).await.unwrap_err();
    assert_custom_error(&err, 65);
}

// Naming the departing feed as retained would make every one of its groups look covered, so nothing
// is unsubscribed while the seat is still released.
#[tokio::test]
async fn test_target_passed_as_retained_rejected() {
    let mut f = setup([100, 0, 0, 38]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1)]).await;
    create_user_on(&mut f, feed, g[0]).await;
    join(&mut f, &[feed], &[g[1]]).await.unwrap();

    let err = leave(&mut f, &[feed], &[feed], &[]).await.unwrap_err();
    assert_custom_error(&err, 65);

    // The seat and both subscriptions survive the rejected call.
    let user = read_user(&mut f).await;
    assert_eq!(user.subscribers, vec![g[0], g[1]]);
    assert_eq!(seat_users(&read_pass(&mut f).await, &feed), 1);
}

// A feed the user never held cannot stand in as retained: overlapping groups would survive the leave
// while the target's seat is released.
#[tokio::test]
async fn test_unheld_feed_as_retained_rejected() {
    let mut f = setup([100, 0, 0, 39]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let held = create_feed(&mut f, "held", exchange, vec![g[0], g[1]]).await;
    let unheld = create_feed(&mut f, "unheld", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(held, 1), seat(unheld, 1)]).await;
    create_user_on(&mut f, held, g[0]).await;
    join(&mut f, &[held], &[g[1]]).await.unwrap();

    let err = leave(&mut f, &[held], &[unheld], &[]).await.unwrap_err();
    assert_custom_error(&err, 65);

    let user = read_user(&mut f).await;
    assert_eq!(user.subscribers, vec![g[0], g[1]]);
    assert_eq!(seat_users(&read_pass(&mut f).await, &held), 1);
}

// The pass admin drops a feed the user still holds (SetAccessPassFeeds never refuses that). The
// stale feed has no seat left to strand, so it no longer has to be named: the user can still leave
// their other feeds, and the stale entry is pruned along the way.
#[tokio::test]
async fn test_leave_succeeds_after_the_pass_drops_a_held_feed() {
    let mut f = setup([100, 0, 0, 41]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed1 = create_feed(&mut f, "feed1", exchange, vec![g[0]]).await;
    let feed2 = create_feed(&mut f, "feed2", exchange, vec![g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed1, 1), seat(feed2, 1)]).await;
    create_user_on(&mut f, feed1, g[0]).await;
    join(&mut f, &[feed2], &[g[1]]).await.unwrap();

    set_pass_feeds(&mut f, vec![seat(feed1, 1)]).await;

    leave(&mut f, &[feed1], &[], &[g[0]]).await.unwrap();

    let user = read_user(&mut f).await;
    assert!(user.feed_pks.is_empty());
    // The stale feed's group stays subscribed until reconciled (or feed2 is named as a target).
    assert_eq!(user.subscribers, vec![g[1]]);
    assert_eq!(seat_users(&read_pass(&mut f).await, &feed1), 0);
}

// A stale feed named as a target unsubscribes its groups, still honoring retained coverage, and
// touches no seat.
#[tokio::test]
async fn test_stale_feed_named_as_target_cleans_up() {
    let mut f = setup([100, 0, 0, 42]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed1 = create_feed(&mut f, "feed1", exchange, vec![g[0]]).await;
    let feed2 = create_feed(&mut f, "feed2", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed1, 1), seat(feed2, 1)]).await;
    create_user_on(&mut f, feed1, g[0]).await;
    join(&mut f, &[feed2], &[g[1]]).await.unwrap();

    set_pass_feeds(&mut f, vec![seat(feed1, 1)]).await;

    // g[0] stays covered by retained feed1; only g[1] departs with the stale feed2.
    leave(&mut f, &[feed2], &[feed1], &[g[1]]).await.unwrap();

    let user = read_user(&mut f).await;
    assert_eq!(user.subscribers, vec![g[0]]);
    assert_eq!(user.feed_pks, vec![feed1]);
    assert_eq!(seat_users(&read_pass(&mut f).await, &feed1), 1);
}

// A stale feed carries no entitlement, so it cannot stand in as retained coverage.
#[tokio::test]
async fn test_stale_feed_as_retained_rejected() {
    let mut f = setup([100, 0, 0, 43]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed1 = create_feed(&mut f, "feed1", exchange, vec![g[0]]).await;
    let feed2 = create_feed(&mut f, "feed2", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed1, 1), seat(feed2, 1)]).await;
    create_user_on(&mut f, feed1, g[0]).await;
    join(&mut f, &[feed2], &[g[1]]).await.unwrap();

    set_pass_feeds(&mut f, vec![seat(feed2, 1)]).await;

    let err = leave(&mut f, &[feed2], &[feed1], &[g[1]])
        .await
        .unwrap_err();
    assert_custom_error(&err, 93);
}

// MAX_FEED_GROUPS is bounded by transaction capacity: a feed at the cap joins in one transaction.
#[tokio::test]
async fn test_feed_at_max_groups_joins_in_one_transaction() {
    let mut f = setup([100, 0, 0, 44]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    // `entry` only exists so the user account can be created (see create_user_on).
    let entry = create_feed(&mut f, "entry", exchange, vec![g[0]]).await;
    let mut big_groups = g[1..5].to_vec();
    let extra = create_groups(&mut f, MAX_FEED_GROUPS - big_groups.len()).await;
    big_groups.extend(extra);
    let big = create_feed(&mut f, "big", exchange, big_groups.clone()).await;
    set_pass_feeds(&mut f, vec![seat(entry, 1), seat(big, 1)]).await;
    create_user_on(&mut f, entry, g[0]).await;

    join(&mut f, &[big], &big_groups).await.unwrap();

    let user = read_user(&mut f).await;
    assert_eq!(user.subscribers.len(), 1 + MAX_FEED_GROUPS);
    assert_eq!(seat_users(&read_pass(&mut f).await, &big), 1);
}

// A join that would take the user past MAX_USER_FEEDS is refused: UserFeedLimitExceeded (103).
#[tokio::test]
async fn test_join_past_the_user_feed_cap_rejected() {
    let mut f = setup([100, 0, 0, 45]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    // Every feed carries the same group, so each join costs its seat and adds no groups.
    let mut feeds = Vec::new();
    for i in 0..=MAX_USER_FEEDS {
        feeds.push(create_feed(&mut f, &format!("feed{i}"), exchange, vec![g[0]]).await);
    }
    let seats = feeds.iter().map(|k| seat(*k, 1)).collect();
    set_pass_feeds(&mut f, seats).await;
    create_user_on(&mut f, feeds[0], g[0]).await;

    for feed in &feeds[1..MAX_USER_FEEDS] {
        join(&mut f, &[*feed], &[]).await.unwrap();
    }
    assert_eq!(read_user(&mut f).await.feed_pks.len(), MAX_USER_FEEDS);

    let err = join(&mut f, &[feeds[MAX_USER_FEEDS]], &[])
        .await
        .unwrap_err();
    assert_custom_error(&err, 103);
}

// A naked CreateUser makes a bare multicast user (no feed, no group), and one
// SubscribeFeed then joins a whole feed and charges its seat.
#[tokio::test]
async fn test_naked_create_then_subscribe_feed() {
    let mut f = setup([100, 0, 0, 46]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0], g[1]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 1)]).await;

    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    execute_transaction(
        &mut f.banks_client,
        recent_blockhash,
        f.program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: f.user_ip,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            ip_proof: None,
        }),
        vec![
            AccountMeta::new(f.user_pubkey, false),
            AccountMeta::new(f.device_pubkey, false),
            AccountMeta::new(f.accesspass_pubkey, false),
            AccountMeta::new(f.globalstate_pubkey, false),
            AccountMeta::new(f.user_tunnel_block, false),
            AccountMeta::new(f.multicast_publisher_block, false),
            AccountMeta::new(f.tunnel_ids, false),
            AccountMeta::new(f.dz_prefix_block, false),
        ],
        &f.payer,
    )
    .await;

    let user = read_user(&mut f).await;
    assert!(user.subscribers.is_empty());
    assert!(user.feed_pks.is_empty());
    assert_eq!(seat_users(&read_pass(&mut f).await, &feed), 0);

    join(&mut f, &[feed], &[g[0], g[1]]).await.unwrap();

    let user = read_user(&mut f).await;
    assert_eq!(user.subscribers, vec![g[0], g[1]]);
    assert_eq!(user.feed_pks, vec![feed]);
    assert_eq!(seat_users(&read_pass(&mut f).await, &feed), 1);
}

// Only CreateUser is idempotent: a duplicate CreateSubscribeUser is still rejected, and neither
// ticks a seat nor changes the user.
#[tokio::test]
async fn test_duplicate_create_subscribe_user_rejected() {
    let mut f = setup([100, 0, 0, 47]).await;
    let (exchange, g) = (f.exchange_pubkey, f.groups.clone());
    let feed = create_feed(&mut f, "feed1", exchange, vec![g[0]]).await;
    set_pass_feeds(&mut f, vec![seat(feed, 2)]).await;
    create_user_on(&mut f, feed, g[0]).await;

    let ip = f.user_ip;
    let err = try_create_user_at(&mut f, ip, feed, g[0])
        .await
        .unwrap_err();
    match err {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::AccountAlreadyInitialized,
        )) => {}
        other => panic!("expected AccountAlreadyInitialized, got {other:?}"),
    }

    assert_eq!(seat_users(&read_pass(&mut f).await, &feed), 1);
    assert_eq!(read_user(&mut f).await.subscribers, vec![g[0]]);
}
