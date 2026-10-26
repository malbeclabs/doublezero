use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_bond_mirror_pda, get_feed_pda, get_globalstate_pda,
        get_multicastgroup_pda, get_permission_pda, get_program_config_pda,
        get_resource_extension_pda,
    },
    processors::{
        bond_mirror::write::BondMirrorWriteArgs,
        feed::{create::FeedCreateArgs, delete::FeedDeleteArgs, update::FeedUpdateArgs},
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        multicastgroup::{
            allowlist::publisher::add::AddMulticastGroupPubAllowlistArgs,
            create::MulticastGroupCreateArgs,
        },
        permission::create::PermissionCreateArgs,
    },
    resource::ResourceType,
    state::{
        accounttype::AccountType,
        bond_mirror::{BondMirror, BondTier},
        feature_flags::FeatureFlag,
        feed::{Feed, FeedChain, FeedStatus},
        permission::permission_flags,
    },
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signer};

mod test_helpers;
use test_helpers::*;

/// Plausible RFC-28 bond terms: a builder, the bond behind it, the spec it conforms to, the SLA
/// it declared, and a 1 Gbps commitment.
fn bonded_args(code: &str, exchange: Pubkey) -> FeedCreateArgs {
    FeedCreateArgs {
        code: code.to_string(),
        name: "Bonded".to_string(),
        exchange,
        groups: vec![Pubkey::new_unique()],
        builder: Pubkey::new_unique(),
        bond_ref: Pubkey::new_unique(),
        spec_id: "top-of-book@v1.0.0".to_string(),
        sla_hash: [9u8; 32],
        committed_rate_bits_per_sec: 1_000_000_000,
        ..Default::default()
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
            feed_chain: None,
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
async fn test_feed_create_and_update_persist_chain() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "shreds", &exchange);
    let accounts = vec![
        AccountMeta::new(feed_pubkey, false),
        AccountMeta::new(globalstate_pubkey, false),
    ];

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "shreds".to_string(),
            name: "Shreds".to_string(),
            exchange,
            groups: vec![Pubkey::new_unique()],
            feed_chain: FeedChain::Solana,
            ..Default::default()
        }),
        accounts.clone(),
        &payer,
    )
    .await;

    let feed = get_feed(&mut banks_client, feed_pubkey).await;
    assert_eq!(feed.feed_chain, FeedChain::Solana);

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateFeed(FeedUpdateArgs {
            name: None,
            groups: None,
            feed_chain: Some(FeedChain::Hyperliquid),
        }),
        accounts,
        &payer,
    )
    .await;

    let feed = get_feed(&mut banks_client, feed_pubkey).await;
    assert_eq!(feed.feed_chain, FeedChain::Hyperliquid);
    assert_eq!(feed.code, "shreds");
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

/// Enable `allow-bonded-feeds` so the RFC-28 create path is reachable.
async fn enable_bonded_feeds(
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
            feature_flags: FeatureFlag::AllowBondedFeeds.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        payer,
    )
    .await;
}

/// The RFC-28 fields ship dormant. Until an operator sets `allow-bonded-feeds` on the cluster, a
/// create carrying a builder is refused outright rather than silently dropping the bond terms.
#[tokio::test]
async fn test_feed_create_bonded_rejected_while_flag_clear() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "dormant", &exchange);

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(bonded_args("dormant", exchange)),
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

/// The bond terms travel together. A builder without the rest describes a feed nothing downstream
/// can measure, so create refuses it rather than storing a half-declared feed.
#[tokio::test]
async fn test_feed_create_partial_bond_terms_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    enable_bonded_feeds(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    // One term dropped per case, each on its own feed code so the PDAs do not collide.
    let mut cases = Vec::new();
    for code in ["nobond", "nospec", "nosla", "norate"] {
        let exchange = Pubkey::new_unique();
        let mut args = bonded_args(code, exchange);
        match code {
            "nobond" => args.bond_ref = Pubkey::default(),
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

/// A bonded feed cannot name a chain. A feed with both a builder and a chain would not show clearly
/// who gets the rewards.
#[tokio::test]
async fn test_feed_create_bonded_with_a_chain_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    enable_bonded_feeds(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "polymarket", &exchange);
    let mut args = bonded_args("polymarket", exchange);
    args.feed_chain = FeedChain::Solana;

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

/// Bond terms without a builder are refused too: they would be recorded against nobody.
#[tokio::test]
async fn test_feed_create_bond_terms_without_builder_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    enable_bonded_feeds(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "orphan", &exchange);
    let mut args = bonded_args("orphan", exchange);
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

/// A bond mirror as the relayer would write it: `tier` covers up to its own ceiling, and the
/// builder declared `committed_rate_bits_per_sec` when it posted the bond.
fn bond_mirror(bond_ref: Pubkey, builder: Pubkey, tier: BondTier, bump_seed: u8) -> Vec<u8> {
    borsh::to_vec(&BondMirror {
        account_type: AccountType::BondMirror,
        owner: Pubkey::new_unique(),
        bump_seed,
        bond_ref,
        builder,
        tier,
        committed_rate_bits_per_sec: tier.max_rate_bits_per_sec(),
        source_slot: 1,
        relayer: Pubkey::new_unique(),
        feed_key: Pubkey::default(),
        groups_created: 0,
    })
    .unwrap()
}

/// Start a cluster with `allow-bonded-feeds` set and one bond already mirrored for `builder`.
/// The returned payer is the foundation key, which is authorized; `builder` is whoever the caller
/// named and holds nothing.
async fn init_bonded_for(
    builder: Pubkey,
    tier: BondTier,
) -> (
    BanksClient,
    Pubkey,
    solana_sdk::signature::Keypair,
    Pubkey,
    Pubkey,
) {
    let program_id = Pubkey::new_unique();
    let bond_ref = Pubkey::new_unique();
    let (mirror_pubkey, bump) = get_bond_mirror_pda(&program_id, &bond_ref);

    let (mut banks_client, payer, recent_blockhash) = init_test_with_accounts(
        program_id,
        &[(mirror_pubkey, bond_mirror(bond_ref, builder, tier, bump))],
    )
    .await;

    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    enable_bonded_feeds(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    (
        banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        bond_ref,
    )
}

/// Start a cluster with `allow-bonded-feeds` set and one bond already mirrored. Returns the
/// builder and the bond it posted; the mirror PDA is seeded by the bond.
async fn init_bonded(
    tier: BondTier,
) -> (
    BanksClient,
    Pubkey,
    solana_sdk::signature::Keypair,
    Pubkey,
    Pubkey,
    Pubkey,
) {
    let builder = Pubkey::new_unique();
    let (banks_client, program_id, payer, globalstate_pubkey, bond_ref) =
        init_bonded_for(builder, tier).await;
    (
        banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        builder,
        bond_ref,
    )
}

/// CreateFeed's fixed accounts. The harness appends payer and system_program, and the bond
/// mirror rides after those as an extra account.
fn feed_accounts(feed: Pubkey, globalstate: Pubkey) -> Vec<AccountMeta> {
    vec![
        AccountMeta::new(feed, false),
        AccountMeta::new(globalstate, false),
    ]
}

/// A bond whose tier covers the rate lets the feed through.
#[tokio::test]
async fn test_feed_create_covered_by_bond_tier() {
    let (mut banks_client, program_id, payer, globalstate_pubkey, builder, bond_ref) =
        init_bonded(BondTier::UpTo5Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "covered", &exchange);
    let (mirror_pubkey, _) = get_bond_mirror_pda(&program_id, &bond_ref);

    let mut args = bonded_args("covered", exchange);
    args.builder = builder;
    args.bond_ref = bond_ref;
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
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    let feed = get_feed(&mut banks_client, feed_pubkey).await;
    assert_eq!(feed.builder, builder);
    assert_eq!(feed.bond_ref, expected.bond_ref);
    assert_eq!(feed.spec_id, expected.spec_id);
    assert_eq!(feed.sla_hash, expected.sla_hash);
    assert_eq!(feed.committed_rate_bits_per_sec, 5_000_000_000);
    // Active at once: the bond checked at create is the admission.
    assert_eq!(feed.status, FeedStatus::Active);

    // The bond is now spent on this feed.
    let mirror = get_account_data(&mut banks_client, mirror_pubkey)
        .await
        .expect("Unable to get BondMirror")
        .get_bond_mirror()
        .unwrap();
    assert_eq!(mirror.feed_key, feed_pubkey);
}

/// An update cannot give a bonded feed a chain, but it can set the chain back to unspecified.
#[tokio::test]
async fn test_feed_update_refuses_a_chain_on_a_bonded_feed() {
    let (mut banks_client, program_id, payer, globalstate_pubkey, builder, bond_ref) =
        init_bonded(BondTier::UpTo5Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "polymarket", &exchange);
    let (mirror_pubkey, _) = get_bond_mirror_pda(&program_id, &bond_ref);

    let mut args = bonded_args("polymarket", exchange);
    args.builder = builder;
    args.bond_ref = bond_ref;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &payer,
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::UpdateFeed(FeedUpdateArgs {
            name: None,
            groups: None,
            feed_chain: Some(FeedChain::Solana),
        }),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &payer,
        &[],
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateFeed(FeedUpdateArgs {
            name: None,
            groups: None,
            feed_chain: Some(FeedChain::Unspecified),
        }),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &payer,
    )
    .await;
    assert_eq!(
        get_feed(&mut banks_client, feed_pubkey).await.feed_chain as u8,
        0
    );
}

/// One bit over the tier ceiling is refused. The bond was sized against the tier, so a rate
/// above it is a feed the bond does not back.
#[tokio::test]
async fn test_feed_create_rate_above_tier_rejected() {
    let (mut banks_client, program_id, payer, globalstate_pubkey, builder, bond_ref) =
        init_bonded(BondTier::UpTo1Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "overtier", &exchange);
    let (mirror_pubkey, _) = get_bond_mirror_pda(&program_id, &bond_ref);

    let mut args = bonded_args("overtier", exchange);
    args.builder = builder;
    args.bond_ref = bond_ref;
    args.committed_rate_bits_per_sec = 1_000_000_001;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &payer,
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::BondDoesNotCoverRate));
}

/// A builder with no mirror at all cannot create a feed. The account is absent, so there is
/// nothing to check the rate against.
#[tokio::test]
async fn test_feed_create_without_bond_mirror_rejected() {
    let (mut banks_client, program_id, payer, globalstate_pubkey, builder, _) =
        init_bonded(BondTier::UpTo5Gbps).await;

    // A bond the relayer has never written a mirror for.
    let unmirrored_bond = Pubkey::new_unique();
    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "nomirror", &exchange);
    let (mirror_pubkey, _) = get_bond_mirror_pda(&program_id, &unmirrored_bond);

    let mut args = bonded_args("nomirror", exchange);
    args.builder = builder;
    args.bond_ref = unmirrored_bond;

    // The account is passed but was never created, so it arrives empty.
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &payer,
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::BondDoesNotCoverRate));
}

/// RFC-28 is one feed per bond: a second feed needs a second bond, and each feed is
/// collateralized on its own, so slashing one never reaches another. The first feed spends the
/// bond, and a second feed pointed at the same one is refused even though the tier still covers
/// its rate.
#[tokio::test]
async fn test_feed_create_second_feed_on_same_bond_rejected() {
    let (mut banks_client, program_id, payer, globalstate_pubkey, builder, bond_ref) =
        init_bonded(BondTier::UpTo5Gbps).await;

    let (mirror_pubkey, _) = get_bond_mirror_pda(&program_id, &bond_ref);
    let exchange = Pubkey::new_unique();

    let (first_pubkey, _) = get_feed_pda(&program_id, "firstfeed", &exchange);
    let mut first = bonded_args("firstfeed", exchange);
    first.builder = builder;
    first.bond_ref = bond_ref;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(first),
        feed_accounts(first_pubkey, globalstate_pubkey),
        &payer,
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    // A different feed, same bond, a rate the tier still covers.
    let (second_pubkey, _) = get_feed_pda(&program_id, "secondfeed", &exchange);
    let mut second = bonded_args("secondfeed", exchange);
    second.builder = builder;
    second.bond_ref = bond_ref;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(second),
        feed_accounts(second_pubkey, globalstate_pubkey),
        &payer,
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::BondAlreadyBacksFeed));
}

/// A bonded feed cannot be deleted. Its bond mirror records this feed to enforce one feed per
/// bond, so closing the feed would leave the builder's bond backing nothing and still refusing
/// to back anything else. Retirement is the path out, and it lands with the lifecycle work.
#[tokio::test]
async fn test_bonded_feed_cannot_be_deleted() {
    let (mut banks_client, program_id, payer, globalstate_pubkey, builder, bond_ref) =
        init_bonded(BondTier::UpTo5Gbps).await;

    let (mirror_pubkey, _) = get_bond_mirror_pda(&program_id, &bond_ref);
    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "nodelete", &exchange);

    let mut args = bonded_args("nodelete", exchange);
    args.builder = builder;
    args.bond_ref = bond_ref;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &payer,
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::DeleteFeed(FeedDeleteArgs {}),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &payer,
        &[],
    )
    .await;

    assert_custom_at_ix0(
        &result,
        custom_code(DoubleZeroError::BondedFeedCannotBeDeleted),
    );

    // Still there, and still holding the bond.
    let feed = get_feed(&mut banks_client, feed_pubkey).await;
    assert_eq!(feed.builder, builder);
}

/// Leaving the mirror account out entirely is refused too, rather than skipping the check.
#[tokio::test]
async fn test_feed_create_omitting_bond_mirror_rejected() {
    let (mut banks_client, program_id, payer, globalstate_pubkey, builder, bond_ref) =
        init_bonded(BondTier::UpTo5Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "omitted", &exchange);

    let mut args = bonded_args("omitted", exchange);
    args.builder = builder;
    args.bond_ref = bond_ref;

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

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::BondMirrorMissing));
}

/// RFC-28 grants a builder publish rights to its own feed's groups. No instruction does that as
/// part of `CreateFeed`, and none needs to: the groups are created with `owner` set to the builder,
/// and `AddMulticastGroupPubAllowlist` authorizes on `mgroup.owner == payer`. So the builder grants
/// its own rights, without the catalog admin who created the feed.
///
/// This walks the whole path with two distinct signers to prove the second step does not need the
/// first one's authority.
#[tokio::test]
async fn test_builder_grants_its_own_publish_rights() {
    let program_id = Pubkey::new_unique();
    // Funded, but not on the foundation allowlist: it cannot create a feed or a multicast group.
    let builder = test_payer();
    let bond_ref = Pubkey::new_unique();
    let (mirror_pubkey, bump) = get_bond_mirror_pda(&program_id, &bond_ref);

    let (mut banks_client, admin, recent_blockhash) = init_test_with_accounts(
        program_id,
        &[(
            mirror_pubkey,
            bond_mirror(bond_ref, builder.pubkey(), BondTier::UpTo1Gbps, bump),
        )],
    )
    .await;

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let (multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    init_globalstate_and_config(&mut banks_client, program_id, &admin, recent_blockhash).await;
    enable_bonded_feeds(&mut banks_client, program_id, globalstate_pubkey, &admin).await;

    // The admin creates the groups, owned by the builder. Group creation needs MULTICAST_ADMIN,
    // which is why the builder cannot do this part itself.
    let mut groups = Vec::new();
    for code in ["mkt-a", "mkt-b"] {
        let gs = get_account_data(&mut banks_client, globalstate_pubkey)
            .await
            .unwrap()
            .get_global_state()
            .unwrap();
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);

        let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                code: code.to_string(),
                max_bandwidth: 1_000_000_000,
                owner: builder.pubkey(),
                use_onchain_allocation: true,
                bond_ref: Pubkey::default(),
            }),
            vec![
                AccountMeta::new(mgroup_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
                AccountMeta::new(multicastgroup_block_pda, false),
            ],
            &admin,
        )
        .await;
        groups.push(mgroup_pubkey);
    }

    // The admin creates the bonded feed over those groups.
    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "rights", &exchange);
    let mut args = bonded_args("rights", exchange);
    args.builder = builder.pubkey();
    args.bond_ref = bond_ref;
    args.groups = groups.clone();

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &admin,
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    // The builder signs from here. The first call creates its access pass; the second adds to it.
    let client_ip = [100, 0, 0, 28].into();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &builder.pubkey());

    for mgroup_pubkey in &groups {
        let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AddMulticastGroupPubAllowlist(
                AddMulticastGroupPubAllowlistArgs {
                    client_ip,
                    user_payer: builder.pubkey(),
                },
            ),
            vec![
                AccountMeta::new(*mgroup_pubkey, false),
                AccountMeta::new(accesspass_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
                AccountMeta::new(builder.pubkey(), false),
            ],
            &builder,
        )
        .await;
    }

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .unwrap()
        .get_accesspass()
        .unwrap();
    for mgroup_pubkey in &groups {
        assert!(
            accesspass.mgroup_pub_allowlist.contains(mgroup_pubkey),
            "builder cannot publish to {mgroup_pubkey}"
        );
    }

    // The feed still holds the same group set, so what the builder may publish to and what the
    // feed sells are the same list.
    let feed = get_feed(&mut banks_client, feed_pubkey).await;
    assert_eq!(feed.groups, groups);
}

/// A relayer updating a mirror must not wipe the feed that claimed the bond.
///
/// The cross-instruction invariant between A3 and A6, and the one a relayer could break by
/// accident. `CreateFeed` writes `feed_key` to spend the bond, which is what makes RFC-28's one
/// feed per bond true. A relayer rebuilding the mirror from Solana alone would zero it and the
/// bond would back two feeds, so `WriteBondMirror` carries the value forward and never takes it
/// from the caller.
#[tokio::test]
async fn test_a_mirror_update_keeps_the_claiming_feed() {
    // Deliberately not `init_bonded`, which plants the mirror by writing account bytes. Every hop
    // here goes through a real instruction, so the create path is exercised too rather than
    // assumed.
    let program_id = Pubkey::new_unique();
    let builder = Pubkey::new_unique();
    let bond_ref = Pubkey::new_unique();

    let (mut banks_client, payer, recent_blockhash) =
        init_test_with_accounts(program_id, &[]).await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    enable_bonded_feeds(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    // The foundation payer grants itself BOND_ORACLE and writes the first mirror, which is what
    // the relayer will do.
    let (permission_pubkey, _) = get_permission_pda(&program_id, &payer.pubkey());
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: payer.pubkey(),
            permissions: permission_flags::BOND_ORACLE,
        }),
        vec![
            AccountMeta::new(permission_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let (mirror_pubkey, _) = get_bond_mirror_pda(&program_id, &bond_ref);
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::WriteBondMirror(BondMirrorWriteArgs {
            bond_ref,
            builder,
            tier: BondTier::UpTo1Gbps,
            committed_rate_bits_per_sec: 1_000_000_000,
            source_slot: 1,
        }),
        vec![
            AccountMeta::new(mirror_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
        &[AccountMeta::new_readonly(permission_pubkey, false)],
    )
    .await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "claimed", &exchange);

    let mut args = bonded_args("claimed", exchange);
    args.builder = builder;
    args.bond_ref = bond_ref;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &payer,
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    let claimed = get_account_data(&mut banks_client, mirror_pubkey)
        .await
        .unwrap()
        .get_bond_mirror()
        .unwrap();
    assert_eq!(claimed.feed_key, feed_pubkey, "the feed spent the bond");

    // The relayer re-mirrors at a newer slot, as it would after seeing the bond change on Solana.
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::WriteBondMirror(BondMirrorWriteArgs {
            bond_ref,
            builder,
            tier: BondTier::Unmetered,
            committed_rate_bits_per_sec: 1_000_000_000,
            source_slot: claimed.source_slot + 1,
        }),
        vec![
            AccountMeta::new(mirror_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
        &[AccountMeta::new_readonly(permission_pubkey, false)],
    )
    .await;

    let after = get_account_data(&mut banks_client, mirror_pubkey)
        .await
        .unwrap()
        .get_bond_mirror()
        .unwrap();
    assert_eq!(after.tier, BondTier::Unmetered, "the update landed");
    assert_eq!(
        after.feed_key, feed_pubkey,
        "the bond is still spent on its feed"
    );
}

/// RFC-28's claim, in one test. A builder holding no permission deploys a feed because it posted a
/// bond, and nothing grants it anything. Before A7 this failed with `NotAllowed`, because the bond
/// bought tier coverage on top of a permission an admin still had to hand over.
#[tokio::test]
async fn test_a_builder_with_no_permission_deploys_against_its_own_bond() {
    let builder = test_payer();
    let (mut banks_client, program_id, _foundation, globalstate_pubkey, bond_ref) =
        init_bonded_for(builder.pubkey(), BondTier::UpTo5Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "ownbond", &exchange);
    let (mirror_pubkey, _) = get_bond_mirror_pda(&program_id, &bond_ref);

    let mut args = bonded_args("ownbond", exchange);
    args.builder = builder.pubkey();
    args.bond_ref = bond_ref;
    args.committed_rate_bits_per_sec = 5_000_000_000;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &builder,
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    let feed = get_feed(&mut banks_client, feed_pubkey).await;
    assert_eq!(feed.builder, builder.pubkey());
    // The builder paid, so the builder owns the feed and can halt it without an admin.
    assert_eq!(feed.owner, builder.pubkey());
    assert_eq!(feed.status, FeedStatus::Active);
}

/// `builder` is a caller-supplied argument, so the bond has to authorize the key that posted it
/// and nobody else. Without that binding, anyone could point at a funded mirror and deploy a feed
/// on the strength of someone else's bond.
#[tokio::test]
async fn test_a_bond_does_not_authorize_a_builder_that_did_not_post_it() {
    let real_builder = Pubkey::new_unique();
    let (mut banks_client, program_id, _foundation, globalstate_pubkey, bond_ref) =
        init_bonded_for(real_builder, BondTier::UpTo5Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "notmine", &exchange);
    let (mirror_pubkey, _) = get_bond_mirror_pda(&program_id, &bond_ref);

    let mut args = bonded_args("notmine", exchange);
    args.builder = real_builder;
    args.bond_ref = bond_ref;
    args.committed_rate_bits_per_sec = 5_000_000_000;

    // test_payer() is funded and holds no permission. The mirror it points at is real and covers
    // the rate; the only thing wrong is that it belongs to someone else.
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &test_payer(),
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
}

/// A builder that has posted no bond has nothing to authorize with, so it falls through to the
/// permission it does not hold.
#[tokio::test]
async fn test_a_builder_with_no_bond_is_refused() {
    let builder = test_payer();
    let (mut banks_client, program_id, _foundation, globalstate_pubkey, _bond_ref) =
        init_bonded_for(builder.pubkey(), BondTier::UpTo5Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "nobond", &exchange);

    let mut args = bonded_args("nobond", exchange);
    args.builder = builder.pubkey();
    // A bond nobody mirrored. No mirror account rides along, so there is nothing to read.
    args.bond_ref = Pubkey::new_unique();
    args.committed_rate_bits_per_sec = 5_000_000_000;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &builder,
        &[],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
}

/// The bond check is deliberately split: whether the caller is a builder speaking about its own
/// bond decides authorization, and whether that bond is big enough decides the feed. So a builder
/// whose tier falls short is told that, rather than being told it is not allowed to try.
#[tokio::test]
async fn test_a_builder_whose_tier_falls_short_is_told_which() {
    let builder = test_payer();
    let (mut banks_client, program_id, _foundation, globalstate_pubkey, bond_ref) =
        init_bonded_for(builder.pubkey(), BondTier::UpTo1Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "toosmall", &exchange);
    let (mirror_pubkey, _) = get_bond_mirror_pda(&program_id, &bond_ref);

    let mut args = bonded_args("toosmall", exchange);
    args.builder = builder.pubkey();
    args.bond_ref = bond_ref;
    args.committed_rate_bits_per_sec = 5_000_000_000;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &builder,
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::BondDoesNotCoverRate));
}

/// The gate reads the mirror rather than trusting one is there.
///
/// The address is derived from `bond_ref`, a caller-supplied argument, so anyone can name a bond
/// nobody posted and attach the empty account its seeds derive. An earlier version of this gate
/// took that for a bond: it skipped `authorize` and answered with a bond error, which told an
/// unauthorized caller apart from an authorized one.
#[tokio::test]
async fn test_an_empty_account_at_the_mirror_address_authorizes_nothing() {
    let builder = test_payer();
    let (mut banks_client, program_id, _foundation, globalstate_pubkey, _bond_ref) =
        init_bonded_for(builder.pubkey(), BondTier::UpTo5Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "hollow", &exchange);

    // A bond nobody posted, and the empty account its seeds derive.
    let invented_bond = Pubkey::new_unique();
    let (hollow_mirror, _) = get_bond_mirror_pda(&program_id, &invented_bond);

    let mut args = bonded_args("hollow", exchange);
    args.builder = builder.pubkey();
    args.bond_ref = invented_bond;
    args.committed_rate_bits_per_sec = 5_000_000_000;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &builder,
        &[AccountMeta::new(hollow_mirror, false)],
    )
    .await;

    // NotAllowed, not a bond error: the caller has no permission and no bond, and must not be
    // able to tell those apart from a caller who has one.
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
}

/// A real mirror belonging to someone else authorizes nothing either, even when the caller names
/// itself as the builder. The mirror's own record is what counts, not the argument.
#[tokio::test]
async fn test_another_builders_mirror_does_not_authorize_the_payer() {
    let other_builder = Pubkey::new_unique();
    let (mut banks_client, program_id, _foundation, globalstate_pubkey, bond_ref) =
        init_bonded_for(other_builder, BondTier::UpTo5Gbps).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "notmine2", &exchange);
    let (mirror_pubkey, _) = get_bond_mirror_pda(&program_id, &bond_ref);

    // The caller names itself, which is what makes `value.builder == payer` true, while the mirror
    // records somebody else.
    let mut args = bonded_args("notmine2", exchange);
    args.builder = test_payer().pubkey();
    args.bond_ref = bond_ref;
    args.committed_rate_bits_per_sec = 5_000_000_000;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        feed_accounts(feed_pubkey, globalstate_pubkey),
        &test_payer(),
        &[AccountMeta::new(mirror_pubkey, false)],
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
}
