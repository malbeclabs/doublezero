use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{get_feed_pda, get_globalstate_pda, get_program_config_pda, get_stake_mirror_pda},
    processors::{
        feed::{create::FeedCreateArgs, delete::FeedDeleteArgs, update::FeedUpdateArgs},
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
    },
    state::{
        accounttype::AccountType,
        feature_flags::FeatureFlag,
        feed::{Feed, FeedStatus},
        stake_mirror::{StakeMirror, StakeTier},
    },
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    program_error::ProgramError,
    pubkey::Pubkey,
    signature::Signer,
    transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

/// The `Custom` code the program returns for `err`, derived from the enum rather than inlined
/// so a renumbering of the error variants can never silently pass a hard-coded literal.
fn custom_code(err: DoubleZeroError) -> u32 {
    match ProgramError::from(err) {
        ProgramError::Custom(code) => code,
        other => panic!("expected Custom, got {other:?}"),
    }
}

/// Run `instruction` and return the structured `TransactionError` on failure so a negative test
/// can match the exact `InstructionError::Custom(code)` at instruction index 0.
///
/// NOTE: this intentionally does not return program logs. With the native `processor!` harness the
/// guest program's `msg!` output is not surfaced to BanksClient, so the structured error code at
/// instruction index 0 is the reliable signal for which check fired.
async fn try_execute_and_get_error(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    instruction: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    payer: &solana_sdk::signature::Keypair,
    extra_accounts: &[AccountMeta],
) -> Result<(), TransactionError> {
    let recent_blockhash = wait_for_new_blockhash(banks_client).await;
    let mut transaction = create_transaction_with_extra_accounts(
        program_id,
        &instruction,
        &accounts,
        payer,
        extra_accounts,
    );
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

/// Plausible RFC-28 stake terms: a builder, the stake behind it, the spec it conforms to, the SLA
/// it declared, and a 1 Gbps commitment.
fn staked_args(code: &str, exchange: Pubkey) -> FeedCreateArgs {
    FeedCreateArgs {
        code: code.to_string(),
        name: "Staked".to_string(),
        exchange,
        groups: vec![Pubkey::new_unique()],
        builder: Pubkey::new_unique(),
        stake_ref: Pubkey::new_unique(),
        spec_id: "top-of-book@v1.0.0".to_string(),
        sla_hash: [9u8; 32],
        committed_rate_bits_per_sec: 1_000_000_000,
    }
}

async fn get_feed(banks_client: &mut BanksClient, feed_pubkey: Pubkey) -> Feed {
    get_account_data(banks_client, feed_pubkey)
        .await
        .expect("Unable to get Feed")
        .get_feed()
        .unwrap()
}

/// Initialize global state so the default payer is on the foundation allowlist and thus authorized
/// for feed catalog instructions.
async fn init_globalstate(
    banks_client: &mut solana_program_test::BanksClient,
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

    globalstate_pubkey
}

#[tokio::test]
async fn test_feed_create_get_update_delete() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "shreds", &exchange);

    let g1 = Pubkey::new_unique();
    let g2 = Pubkey::new_unique();
    let groups = vec![g1, g2];

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "shreds".to_string(),
            name: "Shreds".to_string(),
            exchange,
            groups: groups.clone(),
            ..Default::default()
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let feed = get_account_data(&mut banks_client, feed_pubkey)
        .await
        .expect("Unable to get Feed")
        .get_feed()
        .unwrap();
    assert_eq!(feed.account_type, AccountType::Feed);
    assert_eq!(feed.code, "shreds".to_string());
    assert_eq!(feed.name, "Shreds".to_string());
    assert_eq!(feed.owner, payer.pubkey());
    assert_eq!(feed.exchange, exchange);
    assert_eq!(feed.groups, groups);

    // Update name and groups (exchange is immutable — it's a seed).
    let new_groups = vec![Pubkey::new_unique()];
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateFeed(FeedUpdateArgs {
            name: Some("Shreds v2".to_string()),
            groups: Some(new_groups.clone()),
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let feed = get_account_data(&mut banks_client, feed_pubkey)
        .await
        .expect("Unable to get Feed")
        .get_feed()
        .unwrap();
    assert_eq!(feed.name, "Shreds v2".to_string());
    assert_eq!(feed.exchange, exchange);
    assert_eq!(feed.groups, new_groups);

    // Delete.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteFeed(FeedDeleteArgs {}),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert_eq!(get_account_data(&mut banks_client, feed_pubkey).await, None);
}

#[tokio::test]
async fn test_feed_same_code_different_exchange_allowed() {
    // One code is one SKU offered in many metros; each (code, exchange) is a distinct feed account.
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let tokyo = Pubkey::new_unique();
    let london = Pubkey::new_unique();
    let (tokyo_feed, _) = get_feed_pda(&program_id, "shreds", &tokyo);
    let (london_feed, _) = get_feed_pda(&program_id, "shreds", &london);
    assert_ne!(tokyo_feed, london_feed);

    for (exchange, feed_pubkey) in [(tokyo, tokyo_feed), (london, london_feed)] {
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
                code: "shreds".to_string(),
                name: "Shreds".to_string(),
                exchange,
                groups: vec![Pubkey::new_unique()],
                ..Default::default()
            }),
            vec![
                AccountMeta::new(feed_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;
    }

    for (exchange, feed_pubkey) in [(tokyo, tokyo_feed), (london, london_feed)] {
        let feed = get_account_data(&mut banks_client, feed_pubkey)
            .await
            .expect("Unable to get Feed")
            .get_feed()
            .unwrap();
        assert_eq!(feed.code, "shreds");
        assert_eq!(feed.exchange, exchange);
    }
}

#[tokio::test]
async fn test_feed_create_duplicate_rejected() {
    // The same (code, exchange) derives the same PDA, so a second create hits the already-created
    // account.
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "dupe", &exchange);

    let create = |name: &str| {
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "dupe".to_string(),
            name: name.to_string(),
            exchange,
            groups: vec![Pubkey::new_unique()],
            ..Default::default()
        })
    };

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        create("First"),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        create("Second"),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("AccountAlreadyInitialized"),
        "Expected AccountAlreadyInitialized, got: {error_string}"
    );
}

#[tokio::test]
async fn test_feed_create_empty_groups_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "empty", &exchange);

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "empty".to_string(),
            name: "Empty".to_string(),
            exchange,
            groups: vec![],
            ..Default::default()
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
        &[],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
}

#[tokio::test]
async fn test_feed_create_duplicate_group_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "dupgrp", &exchange);

    let group = Pubkey::new_unique();
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "dupgrp".to_string(),
            name: "Dup group".to_string(),
            exchange,
            groups: vec![group, group],
            ..Default::default()
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
        &[],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
}

#[tokio::test]
async fn test_feed_create_default_exchange_rejected() {
    // Every feed is scoped to a real metro; the default pubkey is not a valid exchange.
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let (feed_pubkey, _) = get_feed_pda(&program_id, "nodef", &Pubkey::default());

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "nodef".to_string(),
            name: "No default".to_string(),
            exchange: Pubkey::default(),
            groups: vec![Pubkey::new_unique()],
            ..Default::default()
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
        &[],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
}

#[tokio::test]
async fn test_feed_create_unauthorized_caller_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "unauth", &exchange);

    // test_payer() is funded but not on the foundation allowlist, so it is not authorized.
    let unauthorized = test_payer();
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "unauth".to_string(),
            name: "Unauthorized".to_string(),
            exchange,
            groups: vec![Pubkey::new_unique()],
            ..Default::default()
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &unauthorized,
        &[],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
}

/// Enable `allow-staked-feeds` so the RFC-28 create path is reachable.
async fn enable_staked_feeds(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &solana_sdk::signature::Keypair,
) {
    let recent_blockhash = wait_for_new_blockhash(banks_client).await;
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::AllowStakedFeeds.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        payer,
    )
    .await;
}

/// The RFC-28 fields ship dormant. Until an operator sets `allow-staked-feeds` on the cluster, a
/// create carrying a builder is refused outright rather than silently dropping the stake terms.
#[tokio::test]
async fn test_feed_create_staked_rejected_while_flag_clear() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "dormant", &exchange);

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(staked_args("dormant", exchange)),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
        &[],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
}

/// A pre-RFC-28 create still works with the flag clear, and the feed it makes is Active: a catalog
/// feed has no builder to attest, so nothing gates it.
#[tokio::test]
async fn test_feed_create_catalog_stays_active_with_flag_clear() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "catalog", &exchange);

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "catalog".to_string(),
            name: "Catalog".to_string(),
            exchange,
            groups: vec![Pubkey::new_unique()],
            ..Default::default()
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let feed = get_feed(&mut banks_client, feed_pubkey).await;
    assert_eq!(feed.status, FeedStatus::Active);
    assert_eq!(feed.builder, Pubkey::default());
}

/// The stake terms travel together. A builder without the rest describes a feed nothing downstream
/// can measure, so create refuses it rather than storing a half-declared feed.
#[tokio::test]
async fn test_feed_create_partial_stake_terms_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    enable_staked_feeds(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    // One term dropped per case, each on its own feed code so the PDAs do not collide.
    let mut cases = Vec::new();
    for code in ["nostake", "nospec", "nosla", "norate"] {
        let exchange = Pubkey::new_unique();
        let mut args = staked_args(code, exchange);
        match code {
            "nostake" => args.stake_ref = Pubkey::default(),
            "nospec" => args.spec_id = String::new(),
            "nosla" => args.sla_hash = [0u8; 32],
            _ => args.committed_rate_bits_per_sec = 0,
        }
        cases.push((get_feed_pda(&program_id, code, &exchange).0, args));
    }

    for (feed_pubkey, args) in cases {
        let result = try_execute_and_get_error(
            &mut banks_client,
            program_id,
            DoubleZeroInstruction::CreateFeed(args),
            vec![
                AccountMeta::new(feed_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
            &[],
        )
        .await;

        assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
    }
}

/// Stake terms without a builder are refused too: they would be recorded against nobody.
#[tokio::test]
async fn test_feed_create_stake_terms_without_builder_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    enable_staked_feeds(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "orphan", &exchange);
    let mut args = staked_args("orphan", exchange);
    args.builder = Pubkey::default();

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
        &[],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
}

/// A stake mirror as the relayer would write it: `tier` covers up to its own ceiling, and the
/// builder declared `committed_rate_bits_per_sec` when it deposited.
fn stake_mirror(builder: Pubkey, tier: StakeTier, bump_seed: u8) -> Vec<u8> {
    borsh::to_vec(&StakeMirror {
        account_type: AccountType::StakeMirror,
        owner: Pubkey::new_unique(),
        bump_seed,
        builder,
        tier,
        committed_rate_bits_per_sec: tier.max_rate_bits_per_sec(),
        source_slot: 1,
        relayer: Pubkey::new_unique(),
    })
    .unwrap()
}

/// Start a cluster with `allow-staked-feeds` set and the builder's stake already mirrored.
async fn init_staked(
    tier: StakeTier,
) -> (
    BanksClient,
    Pubkey,
    solana_sdk::signature::Keypair,
    Pubkey,
    Pubkey,
) {
    let program_id = Pubkey::new_unique();
    let builder = Pubkey::new_unique();
    let (mirror_pubkey, bump) = get_stake_mirror_pda(&program_id, &builder);

    let (mut banks_client, payer, recent_blockhash) = init_test_with_accounts(
        program_id,
        &[(mirror_pubkey, stake_mirror(builder, tier, bump))],
    )
    .await;

    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    enable_staked_feeds(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    (banks_client, program_id, payer, globalstate_pubkey, builder)
}

/// CreateFeed's fixed accounts. The harness appends payer and system_program, and the stake
/// mirror rides after those as an extra account.
fn feed_accounts(feed: Pubkey, globalstate: Pubkey) -> Vec<AccountMeta> {
    vec![
        AccountMeta::new(feed, false),
        AccountMeta::new(globalstate, false),
    ]
}

/// A stake whose tier covers the rate lets the feed through.
#[tokio::test]
async fn test_feed_create_covered_by_stake_tier() {
    let (mut banks_client, program_id, payer, globalstate_pubkey, builder) =
        init_staked(StakeTier::UpTo5Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "covered", &exchange);
    let (mirror_pubkey, _) = get_stake_mirror_pda(&program_id, &builder);

    let mut args = staked_args("covered", exchange);
    args.builder = builder;
    args.committed_rate_bits_per_sec = 5_000_000_000;
    let expected = args.clone();

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &payer,
        &[AccountMeta::new_readonly(mirror_pubkey, false)],
    )
    .await;

    let feed = get_feed(&mut banks_client, feed_pubkey).await;
    assert_eq!(feed.builder, builder);
    assert_eq!(feed.stake_ref, expected.stake_ref);
    assert_eq!(feed.spec_id, expected.spec_id);
    assert_eq!(feed.sla_hash, expected.sla_hash);
    assert_eq!(feed.committed_rate_bits_per_sec, 5_000_000_000);
    // Pending, not Active: the stake is checked at create, but conformance is not.
    assert_eq!(feed.status, FeedStatus::Pending);
}

/// One bit over the tier ceiling is refused. The deposit was sized against the tier, so a rate
/// above it is a feed the stake does not back.
#[tokio::test]
async fn test_feed_create_rate_above_tier_rejected() {
    let (mut banks_client, program_id, payer, globalstate_pubkey, builder) =
        init_staked(StakeTier::UpTo1Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "overtier", &exchange);
    let (mirror_pubkey, _) = get_stake_mirror_pda(&program_id, &builder);

    let mut args = staked_args("overtier", exchange);
    args.builder = builder;
    args.committed_rate_bits_per_sec = 1_000_000_001;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &payer,
        &[AccountMeta::new_readonly(mirror_pubkey, false)],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::StakeDoesNotCoverRate));
}

/// A builder with no mirror at all cannot create a feed. The account is absent, so there is
/// nothing to check the rate against.
#[tokio::test]
async fn test_feed_create_without_stake_mirror_rejected() {
    let (mut banks_client, program_id, payer, globalstate_pubkey, _) =
        init_staked(StakeTier::UpTo5Gbps).await;

    // A builder the relayer has never written a mirror for.
    let unstaked = Pubkey::new_unique();
    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "nomirror", &exchange);
    let (mirror_pubkey, _) = get_stake_mirror_pda(&program_id, &unstaked);

    let mut args = staked_args("nomirror", exchange);
    args.builder = unstaked;

    // The account is passed but was never created, so it arrives empty.
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &payer,
        &[AccountMeta::new_readonly(mirror_pubkey, false)],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::StakeDoesNotCoverRate));
}

/// Leaving the mirror account out entirely is refused too, rather than skipping the check.
#[tokio::test]
async fn test_feed_create_omitting_stake_mirror_rejected() {
    let (mut banks_client, program_id, payer, globalstate_pubkey, builder) =
        init_staked(StakeTier::UpTo5Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "omitted", &exchange);

    let mut args = staked_args("omitted", exchange);
    args.builder = builder;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
        &[],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::StakeMirrorMissing));
}
