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
