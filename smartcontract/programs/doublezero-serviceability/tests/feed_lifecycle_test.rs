//! Halt and resume, and every transition they refuse (RFC-28 D1).
//!
//! The status field and the gate that reads it landed in A5. What was missing was any way to move
//! the status, so a staked feed sat in `Pending` for life and no feed could stop publishing.

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{get_feed_pda, get_globalstate_pda, get_stake_mirror_pda},
    processors::{
        feed::{create::FeedCreateArgs, halt::FeedHaltArgs, resume::FeedResumeArgs},
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
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
};

mod test_helpers;
use test_helpers::*;

const ONE_GBPS: u64 = 1_000_000_000;

/// The accounts every lifecycle instruction takes. The harness appends payer and system program.
fn feed_accounts(feed: Pubkey, globalstate: Pubkey) -> Vec<AccountMeta> {
    vec![
        AccountMeta::new(feed, false),
        AccountMeta::new(globalstate, false),
    ]
}

async fn feed_status(banks_client: &mut BanksClient, feed: Pubkey) -> FeedStatus {
    get_account_data(banks_client, feed)
        .await
        .expect("the feed should exist")
        .get_feed()
        .expect("it should be a feed")
        .status
}

/// A catalog feed: no builder, so it is `Active` the moment it is created. That is the only way to
/// get an `Active` feed today, because `Pending` to `Active` is G1's job and does not exist yet.
async fn catalog_feed(code: &str) -> (BanksClient, Pubkey, Keypair, Pubkey, Pubkey) {
    let program_id = Pubkey::new_unique();
    let (mut banks_client, payer, recent_blockhash) =
        init_test_with_accounts(program_id, &[]).await;
    init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    let (globalstate, _) = get_globalstate_pda(&program_id);

    let exchange = Pubkey::new_unique();
    let (feed, _) = get_feed_pda(&program_id, code, &exchange);
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: code.to_string(),
            name: "Catalog".to_string(),
            exchange,
            groups: vec![Pubkey::new_unique()],
            ..Default::default()
        }),
        feed_accounts(feed, globalstate),
        &payer,
    )
    .await;

    assert_eq!(
        feed_status(&mut banks_client, feed).await,
        FeedStatus::Active,
        "a catalog feed is sellable on creation"
    );

    (banks_client, program_id, payer, globalstate, feed)
}

/// A staked feed, whose builder is a key the test can sign with. It is created `Pending`.
async fn staked_feed_owned_by(
    builder: &Keypair,
    code: &str,
) -> (BanksClient, Pubkey, Keypair, Pubkey, Pubkey) {
    let program_id = Pubkey::new_unique();
    let stake_ref = Pubkey::new_unique();
    let (mirror, bump) = get_stake_mirror_pda(&program_id, &stake_ref);

    let mirror_data = borsh::to_vec(&StakeMirror {
        account_type: AccountType::StakeMirror,
        owner: Pubkey::new_unique(),
        bump_seed: bump,
        stake_ref,
        builder: builder.pubkey(),
        tier: StakeTier::UpTo1Gbps,
        committed_rate_bits_per_sec: ONE_GBPS,
        source_slot: 1,
        relayer: Pubkey::new_unique(),
        feed_key: Pubkey::default(),
    })
    .unwrap();

    let (mut banks_client, payer, recent_blockhash) =
        init_test_with_accounts(program_id, &[(mirror, mirror_data)]).await;
    init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    let (globalstate, _) = get_globalstate_pda(&program_id);

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::AllowStakedFeeds.to_mask(),
        }),
        vec![AccountMeta::new(get_globalstate_pda(&program_id).0, false)],
        &payer,
    )
    .await;

    let exchange = Pubkey::new_unique();
    let (feed, _) = get_feed_pda(&program_id, code, &exchange);
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: code.to_string(),
            name: "Staked".to_string(),
            exchange,
            groups: vec![Pubkey::new_unique()],
            builder: builder.pubkey(),
            stake_ref,
            spec_id: "top-of-book@v1.0.0".to_string(),
            sla_hash: [9u8; 32],
            committed_rate_bits_per_sec: ONE_GBPS,
        }),
        feed_accounts(feed, globalstate),
        &payer,
        &[AccountMeta::new(mirror, false)],
    )
    .await;

    assert_eq!(
        feed_status(&mut banks_client, feed).await,
        FeedStatus::Pending,
        "a staked feed waits on a conformance verdict"
    );

    (banks_client, program_id, payer, globalstate, feed)
}

/// Halt stops publication and resume starts it again, both reversible and neither terminal.
#[tokio::test]
async fn test_a_feed_halts_and_resumes() {
    let (mut banks_client, program_id, payer, globalstate, feed) = catalog_feed("cycle").await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::HaltFeed(FeedHaltArgs {}),
        feed_accounts(feed, globalstate),
        &payer,
    )
    .await;
    assert_eq!(
        feed_status(&mut banks_client, feed).await,
        FeedStatus::Halted
    );

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ResumeFeed(FeedResumeArgs {}),
        feed_accounts(feed, globalstate),
        &payer,
    )
    .await;
    assert_eq!(
        feed_status(&mut banks_client, feed).await,
        FeedStatus::Active,
        "halt is reversible, which is what makes it not retirement"
    );

    // Everything else about the feed is untouched. Halt is a status change, not an edit.
    let feed_account: Feed = get_account_data(&mut banks_client, feed)
        .await
        .unwrap()
        .get_feed()
        .unwrap();
    assert_eq!(feed_account.name, "Catalog");
    assert_eq!(feed_account.groups.len(), 1);
}

/// Halting twice is refused rather than quietly accepted, so a caller cannot mistake a no-op for
/// having stopped something.
#[tokio::test]
async fn test_halting_a_halted_feed_is_refused() {
    let (mut banks_client, program_id, payer, globalstate, feed) = catalog_feed("twice").await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::HaltFeed(FeedHaltArgs {}),
        feed_accounts(feed, globalstate),
        &payer,
    )
    .await;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::HaltFeed(FeedHaltArgs {}),
        feed_accounts(feed, globalstate),
        &payer,
        &[],
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::FeedNotHaltable));
}

/// Resuming a feed that never halted is refused for the same reason.
#[tokio::test]
async fn test_resuming_an_active_feed_is_refused() {
    let (mut banks_client, program_id, payer, globalstate, feed) = catalog_feed("running").await;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::ResumeFeed(FeedResumeArgs {}),
        feed_accounts(feed, globalstate),
        &payer,
        &[],
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::FeedNotResumable));
}

/// A `Pending` feed is not publishing, so there is nothing to halt and nothing to resume.
///
/// This is also the guard on G1's territory: nothing here moves a feed out of `Pending`, because
/// that step re-reads the stake mirror and belongs with the code that does.
#[tokio::test]
async fn test_a_pending_feed_neither_halts_nor_resumes() {
    let builder = test_payer();
    let (mut banks_client, program_id, payer, globalstate, feed) =
        staked_feed_owned_by(&builder, "waiting").await;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::HaltFeed(FeedHaltArgs {}),
        feed_accounts(feed, globalstate),
        &payer,
        &[],
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::FeedNotHaltable));

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::ResumeFeed(FeedResumeArgs {}),
        feed_accounts(feed, globalstate),
        &payer,
        &[],
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::FeedNotResumable));
}

/// The builder is authorized on its own feed, and a stranger is not.
///
/// Both calls fail, because a `Pending` feed cannot be halted by anyone and G1 does not exist yet
/// to make one `Active`. The errors say which check stopped each one: the builder reaches the
/// status check, and the stranger does not get past authorization.
#[tokio::test]
async fn test_the_builder_is_authorized_on_its_own_feed() {
    let builder = test_payer();
    let (mut banks_client, program_id, _payer, globalstate, feed) =
        staked_feed_owned_by(&builder, "mine").await;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::HaltFeed(FeedHaltArgs {}),
        feed_accounts(feed, globalstate),
        &builder,
        &[],
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::FeedNotHaltable));

    let stranger = Keypair::new();
    transfer(&mut banks_client, &builder, &stranger.pubkey(), 10_000_000).await;
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::HaltFeed(FeedHaltArgs {}),
        feed_accounts(feed, globalstate),
        &stranger,
        &[],
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
}

/// A halted staked feed, seeded at genesis, with the mirror that covers it.
///
/// Written rather than driven, because a staked feed cannot reach `Halted` through real
/// instructions yet: it is created `Pending`, and `Pending` to `Active` is G1. The alternative is
/// to leave the rule below untested until G1 lands, which is how the hole this guards against
/// would ship.
fn halted_feed(
    program_id: Pubkey,
    code: &str,
    exchange: Pubkey,
    builder: Pubkey,
    halted_by: Pubkey,
    stake_ref: Pubkey,
) -> (Pubkey, Vec<u8>, Pubkey, Vec<u8>) {
    let (feed_key, bump) = get_feed_pda(&program_id, code, &exchange);
    let feed = Feed {
        account_type: AccountType::Feed,
        owner: Pubkey::new_unique(),
        bump_seed: bump,
        code: code.to_string(),
        name: "Halted".to_string(),
        exchange,
        groups: vec![Pubkey::new_unique()],
        builder,
        stake_ref,
        spec_id: "top-of-book@v1.0.0".to_string(),
        sla_hash: [9u8; 32],
        committed_rate_bits_per_sec: ONE_GBPS,
        status: FeedStatus::Halted,
        halted_by,
    };

    let (mirror_key, mirror_bump) = get_stake_mirror_pda(&program_id, &stake_ref);
    let mirror = StakeMirror {
        account_type: AccountType::StakeMirror,
        owner: Pubkey::new_unique(),
        bump_seed: mirror_bump,
        stake_ref,
        builder,
        tier: StakeTier::UpTo1Gbps,
        committed_rate_bits_per_sec: ONE_GBPS,
        source_slot: 1,
        relayer: Pubkey::new_unique(),
        // The feed already holds the claim; this is not a creation.
        feed_key,
    };

    (
        feed_key,
        borsh::to_vec(&feed).unwrap(),
        mirror_key,
        borsh::to_vec(&mirror).unwrap(),
    )
}

/// Bring up a cluster holding one halted staked feed and its mirror.
async fn cluster_with_halted_feed(
    code: &str,
    builder: Pubkey,
    halted_by: Pubkey,
) -> (BanksClient, Pubkey, Keypair, Pubkey, Pubkey, Pubkey) {
    let program_id = Pubkey::new_unique();
    let (feed_key, feed_data, mirror_key, mirror_data) = halted_feed(
        program_id,
        code,
        Pubkey::new_unique(),
        builder,
        halted_by,
        Pubkey::new_unique(),
    );

    let (mut banks_client, payer, recent_blockhash) = init_test_with_accounts(
        program_id,
        &[(feed_key, feed_data), (mirror_key, mirror_data)],
    )
    .await;
    init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    let (globalstate, _) = get_globalstate_pda(&program_id);

    (
        banks_client,
        program_id,
        payer,
        globalstate,
        feed_key,
        mirror_key,
    )
}

/// An operator's halt is not the builder's to lift.
///
/// Without this, an operator's halt buys nothing: the builder resumes the moment it lands.
/// `Retired` is unreachable until D2 and `DeleteFeed` refuses a staked feed, so the halt is the
/// only lever an operator has, and a builder that can undo it leaves no lever at all.
#[tokio::test]
async fn test_a_builder_cannot_lift_an_operator_halt() {
    let builder = test_payer();
    let operator = Pubkey::new_unique();
    let (mut banks_client, program_id, _payer, globalstate, feed, mirror) =
        cluster_with_halted_feed("seized", builder.pubkey(), operator).await;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::ResumeFeed(FeedResumeArgs {}),
        feed_accounts(feed, globalstate),
        &builder,
        &[AccountMeta::new_readonly(mirror, false)],
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));

    assert_eq!(
        feed_status(&mut banks_client, feed).await,
        FeedStatus::Halted,
        "the halt stands"
    );
}

/// A builder's own halt is the builder's to lift, which is the source rotation RFC-28 asks for.
#[tokio::test]
async fn test_a_builder_lifts_its_own_halt() {
    let builder = test_payer();
    let (mut banks_client, program_id, _payer, globalstate, feed, mirror) =
        cluster_with_halted_feed("rotate", builder.pubkey(), builder.pubkey()).await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ResumeFeed(FeedResumeArgs {}),
        feed_accounts(feed, globalstate),
        &builder,
        &[AccountMeta::new_readonly(mirror, false)],
    )
    .await;

    assert_eq!(
        feed_status(&mut banks_client, feed).await,
        FeedStatus::Active
    );
}

/// Resume re-proves the cover. A stake corrected downward while the feed sat halted must not let
/// it publish again at a rate the stake no longer backs.
#[tokio::test]
async fn test_a_feed_cannot_resume_beyond_its_stake() {
    let builder = test_payer();
    let program_id = Pubkey::new_unique();
    let stake_ref = Pubkey::new_unique();
    let (feed_key, feed_data, mirror_key, _) = halted_feed(
        program_id,
        "shrunk",
        Pubkey::new_unique(),
        builder.pubkey(),
        builder.pubkey(),
        stake_ref,
    );

    // The same mirror, corrected down to a tier that no longer covers the feed's rate.
    let (_, mirror_bump) = get_stake_mirror_pda(&program_id, &stake_ref);
    let shrunk = borsh::to_vec(&StakeMirror {
        account_type: AccountType::StakeMirror,
        owner: Pubkey::new_unique(),
        bump_seed: mirror_bump,
        stake_ref,
        builder: builder.pubkey(),
        tier: StakeTier::None,
        committed_rate_bits_per_sec: 0,
        source_slot: 2,
        relayer: Pubkey::new_unique(),
        feed_key,
    })
    .unwrap();

    let (mut banks_client, payer, recent_blockhash) =
        init_test_with_accounts(program_id, &[(feed_key, feed_data), (mirror_key, shrunk)]).await;
    init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    let (globalstate, _) = get_globalstate_pda(&program_id);

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::ResumeFeed(FeedResumeArgs {}),
        feed_accounts(feed_key, globalstate),
        &builder,
        &[AccountMeta::new_readonly(mirror_key, false)],
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::StakeDoesNotCoverRate));
}

/// A staked feed cannot resume without its mirror. Omitting the account must not read as "no
/// stake to check".
#[tokio::test]
async fn test_a_staked_feed_cannot_resume_without_its_mirror() {
    let builder = test_payer();
    let (mut banks_client, program_id, _payer, globalstate, feed, _mirror) =
        cluster_with_halted_feed("nomirror", builder.pubkey(), builder.pubkey()).await;

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::ResumeFeed(FeedResumeArgs {}),
        feed_accounts(feed, globalstate),
        &builder,
        &[],
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::StakeMirrorMissing));
}
