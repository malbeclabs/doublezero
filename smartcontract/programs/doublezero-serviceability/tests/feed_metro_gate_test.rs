//! Integration tests for the EdgeSeat feed metro gate (#1700).
//!
//! Scenarios:
//! - wrong-metro device rejected (MetroMismatch)
//! - right-metro joins the metro's group set
//! - multi-feed seat (matching feed admits)
//! - group not in the feed's set rejected (GroupNotInFeed)

use doublezero_serviceability::{
    entrypoint::process_instruction,
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_contributor_pda, get_device_pda, get_exchange_pda, get_feed_pda,
        get_globalconfig_pda, get_globalstate_pda, get_location_pda, get_multicastgroup_pda,
        get_resource_extension_pda, get_user_pda,
    },
    processors::{
        accesspass::{
            set::SetAccessPassArgs,
            set_feeds::{FeedSeatConfig, SetAccessPassFeedsArgs},
        },
        contributor::create::ContributorCreateArgs,
        device::{create::DeviceCreateArgs, update::DeviceUpdateArgs},
        exchange::create::ExchangeCreateArgs,
        feed::create::FeedCreateArgs,
        location::create::LocationCreateArgs,
        multicastgroup::create::MulticastGroupCreateArgs,
        user::create_subscribe::UserCreateSubscribeArgs,
    },
    resource::ResourceType,
    state::{
        accesspass::{AccessPassType, FeedSeat},
        device::DeviceType,
        user::{UserCYOA, UserStatus, UserType},
    },
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signer};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

// Far-future billing-window bounds (year ~2096 / ~2099) so the processor's "window_end must be in
// the future" check stays satisfied for the lifetime of these tests.
const TEST_WINDOW_END: i64 = 4_000_000_000;
const TEST_TERMINATES_AT: i64 = 4_100_000_000;

struct FeedFixture {
    banks_client: BanksClient,
    payer: solana_sdk::signature::Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    exchange_pubkey: Pubkey,
    device_pubkey: Pubkey,
    accesspass_pubkey: Pubkey,
    mgroup_pubkey: Pubkey,
    user_ip: Ipv4Addr,
    user_tunnel_block: Pubkey,
    multicast_publisher_block: Pubkey,
    tunnel_ids: Pubkey,
    dz_prefix_block: Pubkey,
}

/// Build GlobalState/Config, Location, Exchange, Contributor, an Activated Device, an Activated
/// MulticastGroup, and an EdgeSeat access pass (no feeds yet — provisioned per test).
async fn setup_feed_fixture(client_ip: [u8; 4]) -> FeedFixture {
    let program_id = Pubkey::new_unique();
    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    );
    program_test.set_compute_max_units(1_000_000);
    let (mut banks_client, payer, recent_blockhash) = program_test.start().await;

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);
    let (user_tunnel_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);

    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, gs.account_index + 1);
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

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, gs.account_index + 1);
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

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, gs.account_index + 1);
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

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
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

    // EdgeSeat access pass with no feeds yet.
    let user_ip: Ipv4Addr = client_ip.into();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user_ip, &payer.pubkey());
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::EdgeSeat(vec![]),
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

    FeedFixture {
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        exchange_pubkey,
        device_pubkey,
        accesspass_pubkey,
        mgroup_pubkey,
        user_ip,
        user_tunnel_block,
        multicast_publisher_block,
        tunnel_ids,
        dz_prefix_block,
    }
}

/// Create a feed (catalog admin = foundation payer) with the given metro map.
async fn create_feed(
    f: &mut FeedFixture,
    code: &str,
    exchange: Pubkey,
    groups: Vec<Pubkey>,
) -> Pubkey {
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

/// Provision the given feed seats onto the access pass via SetAccessPassFeeds.
async fn set_pass_feeds(f: &mut FeedFixture, seats: Vec<FeedSeat>) {
    let recent_blockhash = f.banks_client.get_latest_blockhash().await.unwrap();
    // Account layout: [accesspass, globalstate, feed_0..feed_N, payer, system]. The feed accounts
    // (writable for the reference_count bump) are part of the base list, before the trailing
    // payer/system appended by create_transaction.
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
            client_ip: f.user_ip,
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

/// Attempt CreateSubscribeUser as a subscriber, passing `feed` as the trailing metro-gate account.
async fn try_subscribe_with_feed(
    f: &mut FeedFixture,
    feed: Pubkey,
) -> Result<(), BanksClientError> {
    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    let (user_pubkey, _) = get_user_pda(&f.program_id, &f.user_ip, UserType::Multicast);
    let accounts = vec![
        AccountMeta::new(user_pubkey, false),
        AccountMeta::new(f.device_pubkey, false),
        AccountMeta::new(f.mgroup_pubkey, false),
        AccountMeta::new(f.accesspass_pubkey, false),
        AccountMeta::new(f.globalstate_pubkey, false),
        AccountMeta::new(f.user_tunnel_block, false),
        AccountMeta::new(f.multicast_publisher_block, false),
        AccountMeta::new(f.tunnel_ids, false),
        AccountMeta::new(f.dz_prefix_block, false),
    ];
    let mut tx = create_transaction_with_extra_accounts(
        f.program_id,
        &DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: f.user_ip,
            publisher: false,
            subscriber: true,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            owner: Pubkey::default(),
        }),
        &accounts,
        &f.payer,
        &[AccountMeta::new_readonly(feed, false)],
    );
    tx.try_sign(&[&f.payer], recent_blockhash).unwrap();
    f.banks_client.process_transaction(tx).await
}

#[tokio::test]
async fn test_right_metro_joins_group_set() {
    let mut f = setup_feed_fixture([100, 0, 0, 20]).await;
    let (exchange, mgroup) = (f.exchange_pubkey, f.mgroup_pubkey);
    // Feed serves the device's exchange with [mgroup].
    let feed = create_feed(&mut f, "shreds", exchange, vec![mgroup]).await;
    set_pass_feeds(
        &mut f,
        vec![FeedSeat {
            feed_key: feed,
            max_users: 2,
            max_future_users: 2,
            current_users: 0,
            anniversary_day: 15,
            window_end: TEST_WINDOW_END,
            terminates_at: TEST_TERMINATES_AT,
        }],
    )
    .await;

    try_subscribe_with_feed(&mut f, feed)
        .await
        .expect("right-metro subscribe should succeed");

    let (user_pubkey, _) = get_user_pda(&f.program_id, &f.user_ip, UserType::Multicast);
    let user = get_account_data(&mut f.banks_client, user_pubkey)
        .await
        .expect("user exists")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);
    assert_eq!(user.subscribers, vec![f.mgroup_pubkey]);

    // The feed seat was ticked, and the user records which feed it consumed.
    let pass = get_account_data(&mut f.banks_client, f.accesspass_pubkey)
        .await
        .unwrap()
        .get_accesspass()
        .unwrap();
    assert_eq!(pass.feed_seats()[0].current_users, 1);
    // The user records which feed it consumed, so delete releases exactly that seat
    // (`remove_feed_user(user.feed_pk)`); the decrement itself is covered by the AccessPass unit
    // tests.
    assert_eq!(
        user.feed_pk, feed,
        "connect records the consumed feed on the user"
    );
}

#[tokio::test]
async fn test_wrong_metro_device_rejected() {
    let mut f = setup_feed_fixture([100, 0, 0, 21]).await;
    let mgroup = f.mgroup_pubkey;
    // Feed serves a DIFFERENT exchange, so the device's metro is not covered.
    let other_exchange = Pubkey::new_unique();
    let feed = create_feed(&mut f, "shreds", other_exchange, vec![mgroup]).await;
    set_pass_feeds(
        &mut f,
        vec![FeedSeat {
            feed_key: feed,
            max_users: 2,
            max_future_users: 2,
            current_users: 0,
            anniversary_day: 15,
            window_end: TEST_WINDOW_END,
            terminates_at: TEST_TERMINATES_AT,
        }],
    )
    .await;

    let err = try_subscribe_with_feed(&mut f, feed)
        .await
        .expect_err("wrong-metro subscribe should be rejected");
    assert!(
        format!("{err:?}").contains("Custom(91)"),
        "expected MetroMismatch (Custom(91)), got: {err:?}"
    );
}

#[tokio::test]
async fn test_multi_feed_seat_matching_admits() {
    let mut f = setup_feed_fixture([100, 0, 0, 22]).await;
    let (exchange, mgroup) = (f.exchange_pubkey, f.mgroup_pubkey);
    // Two feeds: one serving a bogus metro, one serving the device's metro.
    let feed_other = create_feed(&mut f, "tokyo", Pubkey::new_unique(), vec![mgroup]).await;
    let feed_match = create_feed(&mut f, "fra", exchange, vec![mgroup]).await;
    set_pass_feeds(
        &mut f,
        vec![
            FeedSeat {
                feed_key: feed_other,
                max_users: 1,
                max_future_users: 1,
                current_users: 0,
                anniversary_day: 15,
                window_end: TEST_WINDOW_END,
                terminates_at: TEST_TERMINATES_AT,
            },
            FeedSeat {
                feed_key: feed_match,
                max_users: 1,
                max_future_users: 1,
                current_users: 0,
                anniversary_day: 15,
                window_end: TEST_WINDOW_END,
                terminates_at: TEST_TERMINATES_AT,
            },
        ],
    )
    .await;

    // Subscribing with the matching feed succeeds.
    try_subscribe_with_feed(&mut f, feed_match)
        .await
        .expect("subscribe via the matching feed should succeed");

    let pass = get_account_data(&mut f.banks_client, f.accesspass_pubkey)
        .await
        .unwrap()
        .get_accesspass()
        .unwrap();
    let matched = pass
        .feed_seats()
        .iter()
        .find(|s| s.feed_key == feed_match)
        .unwrap();
    assert_eq!(matched.current_users, 1);
}

#[tokio::test]
async fn test_group_not_in_feed_rejected() {
    let mut f = setup_feed_fixture([100, 0, 0, 23]).await;
    let exchange = f.exchange_pubkey;
    // Feed serves the device's metro but with a different group set, so the target mgroup is not
    // joinable.
    let other_group = Pubkey::new_unique();
    let feed = create_feed(&mut f, "shreds", exchange, vec![other_group]).await;
    set_pass_feeds(
        &mut f,
        vec![FeedSeat {
            feed_key: feed,
            max_users: 2,
            max_future_users: 2,
            current_users: 0,
            anniversary_day: 15,
            window_end: TEST_WINDOW_END,
            terminates_at: TEST_TERMINATES_AT,
        }],
    )
    .await;

    let err = try_subscribe_with_feed(&mut f, feed)
        .await
        .expect_err("group outside the feed should be rejected");
    assert!(
        format!("{err:?}").contains("Custom(94)"),
        "expected GroupNotInFeed (Custom(94)), got: {err:?}"
    );
}
