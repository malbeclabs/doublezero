//! The RFC-28 sequence, driven end to end through real instructions.
//!
//! Every other test here covers one instruction and seeds whatever it needs around it. This one
//! covers the order: the relayer mirrors a stake, a builder holding no permission creates a feed
//! against it, grants its own publisher the right to send, and an operator admits the feed. That
//! is steps 2 to 5 of the demo, and the first check that they compose rather than each working
//! alone.
//!
//! What it does not cover, and what still has to be proven elsewhere:
//!
//! - **The bond.** `PostBond` is a different program on Solana, so the mirror here is written from
//!   values rather than read from a `BuilderStake`. The relayer decoding a real bond is C2.
//! - **Datagrams.** Nothing in a `ProgramTest` sends multicast. That is G2, against a cluster.
//!
//! So this proves the ledger admits the sequence, not that the demo runs.

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_feed_pda, get_globalstate_pda, get_multicastgroup_pda,
        get_permission_pda, get_resource_extension_pda, get_stake_mirror_pda,
    },
    processors::{
        feed::{activate::FeedActivateArgs, create::FeedCreateArgs},
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        multicastgroup::{
            allowlist::publisher::add::AddMulticastGroupPubAllowlistArgs,
            create::MulticastGroupCreateArgs,
        },
        permission::create::PermissionCreateArgs,
        stake_mirror::write::StakeMirrorWriteArgs,
    },
    resource::ResourceType,
    state::{
        feature_flags::FeatureFlag, feed::FeedStatus, permission::permission_flags,
        stake_mirror::StakeTier,
    },
};
use solana_program_test::*;
use solana_sdk::{
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

const ONE_GBPS: u64 = 1_000_000_000;

/// The whole sequence, in order, with nothing seeded that an instruction could write.
#[tokio::test]
async fn test_a_builder_deploys_a_feed_and_an_operator_admits_it() {
    let program_id = Pubkey::new_unique();

    // The builder holds no permission at any point. That is the claim under test.
    let builder = test_payer();
    let relayer = Keypair::new();
    let stake_ref = Pubkey::new_unique();

    let (mut banks_client, foundation, recent_blockhash) =
        init_test_with_accounts(program_id, &[]).await;
    init_globalstate_and_config(&mut banks_client, program_id, &foundation, recent_blockhash).await;
    let (globalstate, _) = get_globalstate_pda(&program_id);

    transfer(
        &mut banks_client,
        &foundation,
        &relayer.pubkey(),
        100_000_000,
    )
    .await;

    // ---- Operator: turn the RFC-28 path on. -------------------------------------------------
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::AllowStakedFeeds.to_mask(),
        }),
        vec![AccountMeta::new(globalstate, false)],
        &foundation,
    )
    .await;

    // ---- Operator: the group the builder will publish to, owned by the builder. --------------
    let globalstate_account = get_globalstate(&mut banks_client, globalstate).await;
    let (group, _) = get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "tokyo-tob".to_string(),
            max_bandwidth: ONE_GBPS,
            owner: builder.pubkey(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(group, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
        ],
        &foundation,
    )
    .await;

    // ---- Operator: the relayer's key, which is the only thing that may write a mirror. -------
    let (relayer_permission, _) = get_permission_pda(&program_id, &relayer.pubkey());
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: relayer.pubkey(),
            permissions: permission_flags::STAKE_ORACLE,
        }),
        vec![
            AccountMeta::new(relayer_permission, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        &foundation,
    )
    .await;

    // ---- Relayer: the bond reaches the ledger. -----------------------------------------------
    let (mirror, _) = get_stake_mirror_pda(&program_id, &stake_ref);
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::WriteStakeMirror(StakeMirrorWriteArgs {
            stake_ref,
            builder: builder.pubkey(),
            tier: StakeTier::UpTo1Gbps,
            committed_rate_bits_per_sec: ONE_GBPS,
            source_slot: 1,
        }),
        vec![
            AccountMeta::new(mirror, false),
            AccountMeta::new(globalstate, false),
        ],
        &relayer,
        &[AccountMeta::new_readonly(relayer_permission, false)],
    )
    .await;

    // ---- Builder: create the feed. No admin grants it anything. ------------------------------
    let exchange = Pubkey::new_unique();
    let (feed, _) = get_feed_pda(&program_id, "tokyo1", &exchange);
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "tokyo1".to_string(),
            name: "Tokyo top of book".to_string(),
            exchange,
            groups: vec![group],
            builder: builder.pubkey(),
            stake_ref,
            spec_id: "top-of-book@v1.0.0".to_string(),
            sla_hash: [9u8; 32],
            committed_rate_bits_per_sec: ONE_GBPS,
        }),
        vec![
            AccountMeta::new(feed, false),
            AccountMeta::new(globalstate, false),
        ],
        &builder,
        &[AccountMeta::new(mirror, false)],
    )
    .await;

    let written = get_account_data(&mut banks_client, feed)
        .await
        .expect("the feed should exist")
        .get_feed()
        .expect("it should be a feed");
    assert_eq!(written.builder, builder.pubkey());
    assert_eq!(
        written.owner,
        builder.pubkey(),
        "the builder paid, so it owns the feed"
    );
    assert_eq!(
        written.status,
        FeedStatus::Pending,
        "a staked feed waits on a verdict"
    );

    // The stake is spent on this feed, which is what makes one bond back one feed.
    let claimed = get_account_data(&mut banks_client, mirror)
        .await
        .expect("the mirror should exist")
        .get_stake_mirror()
        .unwrap();
    assert_eq!(claimed.feed_key, feed);

    // ---- Builder: grant its own publisher the right to send. ---------------------------------
    let client_ip = Ipv4Addr::new(10, 0, 0, 7);
    let (accesspass, _) = get_accesspass_pda(&program_id, &client_ip, &builder.pubkey());
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip,
            user_payer: builder.pubkey(),
        }),
        vec![
            AccountMeta::new(group, false),
            AccountMeta::new(accesspass, false),
            AccountMeta::new(globalstate, false),
        ],
        &builder,
    )
    .await;

    // ---- Operator: the verdict, and the feed starts selling seats. ---------------------------
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateFeed(FeedActivateArgs {}),
        vec![
            AccountMeta::new(feed, false),
            AccountMeta::new(globalstate, false),
        ],
        &foundation,
        &[AccountMeta::new_readonly(mirror, false)],
    )
    .await;

    let admitted = get_account_data(&mut banks_client, feed)
        .await
        .expect("the feed should exist")
        .get_feed()
        .expect("it should be a feed");
    assert_eq!(admitted.status, FeedStatus::Active);

    // The builder never held a permission. If it had, this sequence would prove nothing.
    assert!(
        get_account_data(
            &mut banks_client,
            get_permission_pda(&program_id, &builder.pubkey()).0
        )
        .await
        .is_none(),
        "the builder must have deployed on its bond, not on a permission"
    );
}
