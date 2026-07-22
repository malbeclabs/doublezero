use doublezero_serviceability::{
    entrypoint::process_instruction,
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    min_version::{EDGE_SEAT_MIN_COMPATIBLE_VERSION, MIN_COMPATIBLE_VERSION},
    pda::{get_accesspass_pda, get_feed_pda, get_globalstate_pda, get_program_config_pda},
    processors::{
        accesspass::{
            set::SetAccessPassArgs,
            set_feeds::{FeedSeatConfig, SetAccessPassFeedsArgs, MAX_ACCESS_PASS_FEEDS},
        },
        feed::create::FeedCreateArgs,
    },
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType, FeedSeat},
        accounttype::AccountType,
        feed::Feed,
        globalstate::GlobalState,
    },
};
use solana_program_test::*;
use solana_sdk::{
    account::Account,
    clock::Clock,
    instruction::{AccountMeta, InstructionError},
    program_error::ProgramError,
    pubkey::Pubkey,
    rent::Rent,
    signature::Signer,
    transaction::TransactionError,
};
use std::net::Ipv4Addr;

// Billing-window bounds for the happy-path tests. Far in the future (year ~2096 / ~2099) so the
// processor's "window_end must be in the future" check stays satisfied for the lifetime of these
// tests, regardless of the bank clock. Tests that exercise the future check itself read the clock
// and derive timestamps relative to it instead.
const TEST_WINDOW_END: i64 = 4_000_000_000;
const TEST_TERMINATES_AT: i64 = 4_100_000_000;

mod test_helpers;
use test_helpers::*;

/// The `Custom` code the program returns for `err`, derived from the enum rather than inlined so a
/// renumbering of the error variants can never silently pass a hard-coded literal.
fn custom_code(err: DoubleZeroError) -> u32 {
    match ProgramError::from(err) {
        ProgramError::Custom(code) => code,
        other => panic!("expected Custom, got {other:?}"),
    }
}

/// Run `instruction` and return the structured `TransactionError` on failure. As documented in
/// `feed_test.rs`, the native `processor!` harness does not surface the program's `msg!` lines to
/// BanksClient, so the structured error code at instruction index 0 is the reliable signal for
/// which check fired.
async fn try_execute_and_get_error(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    instruction: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    payer: &solana_sdk::signature::Keypair,
) -> Result<(), TransactionError> {
    let recent_blockhash = wait_for_new_blockhash(banks_client).await;
    let mut transaction = create_transaction(program_id, &instruction, &accounts, payer);
    transaction.try_sign(&[payer], recent_blockhash).unwrap();

    banks_client
        .process_transaction_with_metadata(transaction)
        .await
        .expect("banks client failed")
        .result
}

/// Assert `result` failed at instruction index 0 with `Custom(expected)`.
fn assert_custom_at_ix0(result: &Result<(), TransactionError>, expected: u32) {
    match result {
        Err(TransactionError::InstructionError(0, InstructionError::Custom(code))) => {
            assert_eq!(*code, expected, "unexpected custom error code");
        }
        other => panic!("expected Custom({expected}) at instruction 0, got {other:?}"),
    }
}

fn serialized_account(program_id: Pubkey, data: Vec<u8>) -> Account {
    Account {
        lamports: Rent::default().minimum_balance(data.len()).max(1),
        data,
        owner: program_id,
        executable: false,
        rent_epoch: 0,
    }
}

/// Initialize global state so the default payer is on the foundation allowlist and thus authorized
/// (via the ACCESS_PASS_ADMIN legacy fallback) for SetAccessPassFeeds, then raise
/// `min_compatible_version` to the EdgeSeat floor so EdgeSeat passes may be written.
async fn init_globalstate(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    payer: &solana_sdk::signature::Keypair,
    recent_blockhash: solana_program::hash::Hash,
) -> Pubkey {
    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    set_min_compatible_version(
        banks_client,
        recent_blockhash,
        program_id,
        payer,
        EDGE_SEAT_MIN_COMPATIBLE_VERSION,
    )
    .await;

    globalstate_pubkey
}

async fn create_feed(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &solana_sdk::signature::Keypair,
    recent_blockhash: solana_program::hash::Hash,
    code: &str,
) -> Pubkey {
    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, code, &exchange);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: code.to_string(),
            name: code.to_string(),
            exchange,
            groups: vec![Pubkey::new_unique()],
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;
    feed_pubkey
}

#[allow(clippy::too_many_arguments)]
async fn create_edge_seat_pass(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &solana_sdk::signature::Keypair,
    recent_blockhash: solana_program::hash::Hash,
    client_ip: Ipv4Addr,
    user_payer: Pubkey,
    accesspass_type: AccessPassType,
) -> Pubkey {
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type,
            client_ip,
            last_access_epoch: u64::MAX,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 4,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        payer,
    )
    .await;
    accesspass_pubkey
}

async fn read_accesspass(banks_client: &mut BanksClient, pubkey: Pubkey) -> AccessPass {
    get_account_data(banks_client, pubkey)
        .await
        .expect("accesspass missing")
        .get_accesspass()
        .unwrap()
}

#[tokio::test]
async fn test_set_access_pass_feeds() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let feed_a = create_feed(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        "feda",
    )
    .await;
    let feed_b = create_feed(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        "fedb",
    )
    .await;

    let client_ip = Ipv4Addr::new(100, 0, 0, 1);
    let user_payer = Pubkey::new_unique();
    let accesspass_pubkey = create_edge_seat_pass(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        client_ip,
        user_payer,
        AccessPassType::EdgeSeat(vec![]),
    )
    .await;

    // Provision two feeds.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![
                FeedSeatConfig {
                    max_users: 5,
                    max_future_users: 5,
                    anniversary_day: 15,
                    window_end: TEST_WINDOW_END,
                    terminates_at: TEST_TERMINATES_AT,
                },
                FeedSeatConfig {
                    max_users: 3,
                    max_future_users: 3,
                    anniversary_day: 15,
                    window_end: TEST_WINDOW_END,
                    terminates_at: TEST_TERMINATES_AT,
                },
            ],
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(feed_a, false),
            AccountMeta::new(feed_b, false),
        ],
        &payer,
    )
    .await;

    let accesspass = read_accesspass(&mut banks_client, accesspass_pubkey).await;
    assert_eq!(
        accesspass.accesspass_type,
        AccessPassType::EdgeSeat(vec![
            FeedSeat {
                feed_key: feed_a,
                max_users: 5,
                max_future_users: 5,
                current_users: 0,
                anniversary_day: 15,
                window_end: TEST_WINDOW_END,
                terminates_at: TEST_TERMINATES_AT,
            },
            FeedSeat {
                feed_key: feed_b,
                max_users: 3,
                max_future_users: 3,
                current_users: 0,
                anniversary_day: 15,
                window_end: TEST_WINDOW_END,
                terminates_at: TEST_TERMINATES_AT,
            },
        ])
    );

    // Re-provision with only feed_a at a higher cap: feed_b is dropped from the pass and the seat
    // cap updates.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![FeedSeatConfig {
                max_users: 7,
                max_future_users: 7,
                anniversary_day: 15,
                window_end: TEST_WINDOW_END,
                terminates_at: TEST_TERMINATES_AT,
            }],
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(feed_a, false),
        ],
        &payer,
    )
    .await;

    let accesspass = read_accesspass(&mut banks_client, accesspass_pubkey).await;
    assert_eq!(
        accesspass.accesspass_type,
        AccessPassType::EdgeSeat(vec![FeedSeat {
            feed_key: feed_a,
            max_users: 7,
            max_future_users: 7,
            current_users: 0,
            anniversary_day: 15,
            window_end: TEST_WINDOW_END,
            terminates_at: TEST_TERMINATES_AT,
        }])
    );
}

#[tokio::test]
async fn test_cannot_set_feeds_on_non_edge_seat() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let feed_a = create_feed(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        "feda",
    )
    .await;

    let client_ip = Ipv4Addr::new(100, 0, 0, 2);
    let user_payer = Pubkey::new_unique();
    let accesspass_pubkey = create_edge_seat_pass(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        client_ip,
        user_payer,
        AccessPassType::Prepaid,
    )
    .await;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![FeedSeatConfig {
                max_users: 5,
                max_future_users: 5,
                anniversary_day: 15,
                window_end: TEST_WINDOW_END,
                terminates_at: TEST_TERMINATES_AT,
            }],
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(feed_a, false),
        ],
        &payer,
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
}

#[tokio::test]
async fn test_cannot_set_feeds_unauthorized_caller() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let feed_a = create_feed(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        "feda",
    )
    .await;

    let client_ip = Ipv4Addr::new(100, 0, 0, 5);
    let user_payer = Pubkey::new_unique();
    let accesspass_pubkey = create_edge_seat_pass(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        client_ip,
        user_payer,
        AccessPassType::EdgeSeat(vec![]),
    )
    .await;

    // test_payer() is funded but not on the foundation allowlist and holds no ACCESS_PASS_ADMIN
    // Permission, so it is not authorized to provision feeds.
    let unauthorized = test_payer();
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![FeedSeatConfig {
                max_users: 5,
                max_future_users: 5,
                anniversary_day: 15,
                window_end: TEST_WINDOW_END,
                terminates_at: TEST_TERMINATES_AT,
            }],
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(feed_a, false),
        ],
        &unauthorized,
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
}

#[tokio::test]
async fn test_cannot_set_feeds_exceeds_max() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let client_ip = Ipv4Addr::new(100, 0, 0, 6);
    let user_payer = Pubkey::new_unique();
    let accesspass_pubkey = create_edge_seat_pass(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        client_ip,
        user_payer,
        AccessPassType::EdgeSeat(vec![]),
    )
    .await;

    // MAX_ACCESS_PASS_FEEDS + 1 configs is rejected by the cap check before any Feed account is
    // read, so no Feed accounts are passed.
    let too_many = vec![
        FeedSeatConfig {
            max_users: 1,
            max_future_users: 1,
            anniversary_day: 15,
            window_end: TEST_WINDOW_END,
            terminates_at: TEST_TERMINATES_AT,
        };
        MAX_ACCESS_PASS_FEEDS + 1
    ];
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: too_many,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
}

#[tokio::test]
async fn test_cannot_set_duplicate_feed_key() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let feed_a = create_feed(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        "feda",
    )
    .await;

    let client_ip = Ipv4Addr::new(100, 0, 0, 3);
    let user_payer = Pubkey::new_unique();
    let accesspass_pubkey = create_edge_seat_pass(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        client_ip,
        user_payer,
        AccessPassType::EdgeSeat(vec![]),
    )
    .await;

    // Same feed listed twice.
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![
                FeedSeatConfig {
                    max_users: 5,
                    max_future_users: 5,
                    anniversary_day: 15,
                    window_end: TEST_WINDOW_END,
                    terminates_at: TEST_TERMINATES_AT,
                },
                FeedSeatConfig {
                    max_users: 2,
                    max_future_users: 2,
                    anniversary_day: 15,
                    window_end: TEST_WINDOW_END,
                    terminates_at: TEST_TERMINATES_AT,
                },
            ],
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(feed_a, false),
            AccountMeta::new(feed_a, false),
        ],
        &payer,
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
}

#[tokio::test]
async fn test_cannot_set_max_users_below_current_users() {
    // A seat with live users can't be re-provisioned below its current_users. current_users is only
    // ticked by connect-time enforcement (a later PR), so the pass is seeded directly here.
    let program_id = Pubkey::new_unique();
    let authority = test_payer();

    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    );

    let (globalstate_pubkey, globalstate_bump) = get_globalstate_pda(&program_id);
    let globalstate = GlobalState {
        bump_seed: globalstate_bump,
        foundation_allowlist: vec![authority.pubkey()],
        // The EdgeSeat compatibility gate reads this; seed it at the floor.
        min_compatible_version: EDGE_SEAT_MIN_COMPATIBLE_VERSION.parse().unwrap(),
        ..GlobalState::default()
    };
    program_test.add_account(
        globalstate_pubkey,
        serialized_account(program_id, borsh::to_vec(&globalstate).unwrap()),
    );

    let feed_exchange = Pubkey::new_unique();
    let (feed_pubkey, feed_bump) = get_feed_pda(&program_id, "livd", &feed_exchange);
    let feed = Feed {
        account_type: AccountType::Feed,
        owner: authority.pubkey(),
        bump_seed: feed_bump,
        code: "livd".to_string(),
        name: "Live".to_string(),
        exchange: feed_exchange,
        groups: vec![Pubkey::new_unique()],
    };
    program_test.add_account(
        feed_pubkey,
        serialized_account(program_id, borsh::to_vec(&feed).unwrap()),
    );

    let client_ip = Ipv4Addr::new(100, 0, 0, 4);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, accesspass_bump) =
        get_accesspass_pda(&program_id, &client_ip, &user_payer);
    let accesspass = AccessPass {
        account_type: AccountType::AccessPass,
        owner: authority.pubkey(),
        bump_seed: accesspass_bump,
        accesspass_type: AccessPassType::EdgeSeat(vec![FeedSeat {
            feed_key: feed_pubkey,
            max_users: 5,
            max_future_users: 5,
            current_users: 3,
            anniversary_day: 15,
            window_end: TEST_WINDOW_END,
            terminates_at: TEST_TERMINATES_AT,
        }]),
        client_ip,
        user_payer,
        last_access_epoch: u64::MAX,
        connection_count: 0,
        status: AccessPassStatus::Connected,
        mgroup_pub_allowlist: vec![],
        mgroup_sub_allowlist: vec![],
        flags: 0,
        tenant_allowlist: vec![],
        unicast_user_count: 0,
        max_unicast_users: 1,
        multicast_user_count: 3,
        max_multicast_users: 4,
    };
    program_test.add_account(
        accesspass_pubkey,
        serialized_account(program_id, borsh::to_vec(&accesspass).unwrap()),
    );

    program_test.add_account(
        authority.pubkey(),
        Account {
            lamports: 100_000_000,
            data: vec![],
            owner: solana_system_interface::program::ID,
            executable: false,
            rent_epoch: 0,
        },
    );

    let (mut banks_client, _payer, _recent_blockhash) = program_test.start().await;

    // max_users (2) below current_users (3) must reject.
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![FeedSeatConfig {
                max_users: 2,
                max_future_users: 2,
                anniversary_day: 15,
                window_end: TEST_WINDOW_END,
                terminates_at: TEST_TERMINATES_AT,
            }],
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(feed_pubkey, false),
        ],
        &authority,
    )
    .await;

    assert_custom_at_ix0(
        &result,
        custom_code(DoubleZeroError::FeedMaxUsersBelowCurrentUsers),
    );
}

#[tokio::test]
async fn test_cannot_set_feeds_with_invalid_billing_window() {
    // The per-feed billing fields are validated inside the provisioning loop, each with its own
    // error variant: anniversary_day must be 1..=31, and the window must satisfy
    // now < window_end <= terminates_at (in the future and ordered).
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let feed_a = create_feed(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        "feda",
    )
    .await;

    let client_ip = Ipv4Addr::new(100, 0, 0, 7);
    let user_payer = Pubkey::new_unique();
    let accesspass_pubkey = create_edge_seat_pass(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        client_ip,
        user_payer,
        AccessPassType::EdgeSeat(vec![]),
    )
    .await;

    let accounts = vec![
        AccountMeta::new(accesspass_pubkey, false),
        AccountMeta::new(globalstate_pubkey, false),
        AccountMeta::new(feed_a, false),
    ];

    // anniversary_day = 0 (below range) rejects.
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![FeedSeatConfig {
                max_users: 5,
                max_future_users: 5,
                anniversary_day: 0,
                window_end: TEST_WINDOW_END,
                terminates_at: TEST_TERMINATES_AT,
            }],
        }),
        accounts.clone(),
        &payer,
    )
    .await;
    assert_custom_at_ix0(
        &result,
        custom_code(DoubleZeroError::FeedInvalidAnniversaryDay),
    );

    // anniversary_day = 32 (above range) rejects.
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![FeedSeatConfig {
                max_users: 5,
                max_future_users: 5,
                anniversary_day: 32,
                window_end: TEST_WINDOW_END,
                terminates_at: TEST_TERMINATES_AT,
            }],
        }),
        accounts.clone(),
        &payer,
    )
    .await;
    assert_custom_at_ix0(
        &result,
        custom_code(DoubleZeroError::FeedInvalidAnniversaryDay),
    );

    // window_end after terminates_at rejects (both far-future so only the ordering fails).
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![FeedSeatConfig {
                max_users: 5,
                max_future_users: 5,
                anniversary_day: 15,
                window_end: TEST_TERMINATES_AT + 1,
                terminates_at: TEST_TERMINATES_AT,
            }],
        }),
        accounts.clone(),
        &payer,
    )
    .await;
    assert_custom_at_ix0(
        &result,
        custom_code(DoubleZeroError::FeedInvalidBillingWindow),
    );

    // Unset (zero) window is not in the future, so it rejects.
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![FeedSeatConfig {
                max_users: 5,
                max_future_users: 5,
                anniversary_day: 15,
                window_end: 0,
                terminates_at: 0,
            }],
        }),
        accounts.clone(),
        &payer,
    )
    .await;
    assert_custom_at_ix0(
        &result,
        custom_code(DoubleZeroError::FeedInvalidBillingWindow),
    );

    // A positive but already-elapsed window_end rejects: this is the "past date slips through" case
    // that motivated the Clock check. Read the bank clock and set window_end just before now.
    let now = banks_client
        .get_sysvar::<Clock>()
        .await
        .unwrap()
        .unix_timestamp;
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![FeedSeatConfig {
                max_users: 5,
                max_future_users: 5,
                anniversary_day: 15,
                window_end: now - 1,
                terminates_at: TEST_TERMINATES_AT,
            }],
        }),
        accounts,
        &payer,
    )
    .await;
    assert_custom_at_ix0(
        &result,
        custom_code(DoubleZeroError::FeedInvalidBillingWindow),
    );
}

#[tokio::test]
async fn test_cannot_set_max_future_users_below_max_users() {
    // For now the future cap may not drop below the current cap (see set_feeds.rs): shrinking it
    // would force a decision about which live users to drop when the cap flips. Equal or larger is
    // allowed (covered by test_set_access_pass_feeds); below must reject.
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let feed_a = create_feed(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        "feda",
    )
    .await;

    let client_ip = Ipv4Addr::new(100, 0, 0, 9);
    let user_payer = Pubkey::new_unique();
    let accesspass_pubkey = create_edge_seat_pass(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        client_ip,
        user_payer,
        AccessPassType::EdgeSeat(vec![]),
    )
    .await;

    // max_future_users (3) below max_users (5) must reject.
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![FeedSeatConfig {
                max_users: 5,
                max_future_users: 3,
                anniversary_day: 15,
                window_end: TEST_WINDOW_END,
                terminates_at: TEST_TERMINATES_AT,
            }],
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(feed_a, false),
        ],
        &payer,
    )
    .await;
    assert_custom_at_ix0(
        &result,
        custom_code(DoubleZeroError::FeedMaxFutureUsersBelowMaxUsers),
    );
}

#[tokio::test]
async fn test_cannot_set_zero_max_users() {
    // A zero current cap admits no users, so a seat with max_users == 0 is rejected as nonsensical.
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let feed_a = create_feed(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        "feda",
    )
    .await;

    let client_ip = Ipv4Addr::new(100, 0, 0, 10);
    let user_payer = Pubkey::new_unique();
    let accesspass_pubkey = create_edge_seat_pass(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        client_ip,
        user_payer,
        AccessPassType::EdgeSeat(vec![]),
    )
    .await;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![FeedSeatConfig {
                max_users: 0,
                max_future_users: 0,
                anniversary_day: 15,
                window_end: TEST_WINDOW_END,
                terminates_at: TEST_TERMINATES_AT,
            }],
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(feed_a, false),
        ],
        &payer,
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::FeedMaxUsersZero));
}

/// While `min_compatible_version` still admits pre-FeedSeat clients, EdgeSeat-typed SetAccessPass
/// and SetAccessPassFeeds are refused; other pass types are unaffected, and re-raising the floor
/// re-admits EdgeSeat writes.
#[tokio::test]
async fn test_edge_seat_blocked_below_compat_floor() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let feed_a = create_feed(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        "feda",
    )
    .await;

    // Created while the floor is at the EdgeSeat level (raised by init_globalstate).
    let client_ip = Ipv4Addr::new(100, 0, 0, 11);
    let user_payer = Pubkey::new_unique();
    let accesspass_pubkey = create_edge_seat_pass(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        client_ip,
        user_payer,
        AccessPassType::EdgeSeat(vec![]),
    )
    .await;

    // Lower the floor back to the init default: pre-FeedSeat clients are admitted again.
    set_min_compatible_version(
        &mut banks_client,
        recent_blockhash,
        program_id,
        &payer,
        MIN_COMPATIBLE_VERSION,
    )
    .await;

    // EdgeSeat-typed SetAccessPass is refused below the floor.
    let blocked_ip = Ipv4Addr::new(100, 0, 0, 12);
    let blocked_payer = Pubkey::new_unique();
    let (blocked_pubkey, _) = get_accesspass_pda(&program_id, &blocked_ip, &blocked_payer);
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::EdgeSeat(vec![]),
            client_ip: blocked_ip,
            last_access_epoch: u64::MAX,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 4,
        }),
        vec![
            AccountMeta::new(blocked_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(blocked_payer, false),
        ],
        &payer,
    )
    .await;
    assert_custom_at_ix0(
        &result,
        custom_code(DoubleZeroError::EdgeSeatCompatibilityWindowNotMet),
    );

    // SetAccessPassFeeds on the existing pass is refused below the floor.
    let feeds = vec![FeedSeatConfig {
        max_users: 5,
        max_future_users: 5,
        anniversary_day: 15,
        window_end: TEST_WINDOW_END,
        terminates_at: TEST_TERMINATES_AT,
    }];
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: feeds.clone(),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(feed_a, false),
        ],
        &payer,
    )
    .await;
    assert_custom_at_ix0(
        &result,
        custom_code(DoubleZeroError::EdgeSeatCompatibilityWindowNotMet),
    );

    // Non-EdgeSeat passes are unaffected by the floor.
    create_edge_seat_pass(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        recent_blockhash,
        Ipv4Addr::new(100, 0, 0, 13),
        Pubkey::new_unique(),
        AccessPassType::Prepaid,
    )
    .await;

    // Raising the floor re-admits EdgeSeat writes.
    set_min_compatible_version(
        &mut banks_client,
        recent_blockhash,
        program_id,
        &payer,
        EDGE_SEAT_MIN_COMPATIBLE_VERSION,
    )
    .await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(feed_a, false),
        ],
        &payer,
    )
    .await;
    let accesspass = read_accesspass(&mut banks_client, accesspass_pubkey).await;
    assert_eq!(accesspass.feed_seats().len(), 1);
}
