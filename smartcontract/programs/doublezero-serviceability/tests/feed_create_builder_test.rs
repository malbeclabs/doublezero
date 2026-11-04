//! A bonded `CreateFeed` built by `doublezero-serviceability-instruction`'s `create_feed`, the
//! builder the SDK's `CreateFeedCommand` and `doublezero feed create --bond-ref` use.
//!
//! `rfc29_walkthrough_test` builds the same create by hand. This proves the account list the
//! public builder emits is one the program accepts: the bond mirror after the system program, and
//! the payer's Permission PDA last, never created.

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{
        get_bond_mirror_pda, get_feed_pda, get_globalstate_pda, get_multicastgroup_pda,
        get_permission_pda,
    },
    processors::{
        bond_mirror::write::BondMirrorWriteArgs, feed::create::FeedCreateArgs,
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        permission::create::PermissionCreateArgs,
    },
    state::{
        bond_mirror::BondTier,
        feature_flags::FeatureFlag,
        feed::{FeedChain, FeedStatus},
        permission::permission_flags,
    },
};
use doublezero_serviceability_instruction as dzi;
use solana_program_test::*;
use solana_sdk::{
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
};

mod test_helpers;
use test_helpers::*;

const TIER: BondTier = BondTier::UpTo1Gbps;

#[tokio::test]
async fn test_the_create_feed_builder_deploys_a_bonded_feed() {
    deploy_bonded_feed([9u8; 32]).await;
}

/// A builder with no service-level document leaves the hash unset. All zeros is "none declared",
/// and the feed is created like any other.
#[tokio::test]
async fn test_a_bonded_feed_need_not_declare_a_service_level() {
    deploy_bonded_feed([0u8; 32]).await;
}

async fn deploy_bonded_feed(sla_hash: [u8; 32]) {
    let program_id = Pubkey::new_unique();
    // The builder holds no permission: the bond alone admits the create.
    let builder = test_payer();
    let relayer = Keypair::new();
    let bond_ref = Pubkey::new_unique();

    // The builder's own group on the bond, which the feed lists.
    let (group, _) = get_multicastgroup_pda(&program_id, 9_999);
    let (mut banks_client, foundation, recent_blockhash) = init_test_with_accounts(
        program_id,
        &[(group, multicast_group(builder.pubkey(), bond_ref))],
    )
    .await;
    init_globalstate_and_config(&mut banks_client, program_id, &foundation, recent_blockhash).await;
    let (globalstate, _) = get_globalstate_pda(&program_id);
    transfer(
        &mut banks_client,
        &foundation,
        &relayer.pubkey(),
        100_000_000,
    )
    .await;

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::AllowBondedFeeds.to_mask(),
        }),
        vec![AccountMeta::new(globalstate, false)],
        &foundation,
    )
    .await;

    let (relayer_permission, _) = get_permission_pda(&program_id, &relayer.pubkey());
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: relayer.pubkey(),
            permissions: permission_flags::BOND_ORACLE,
        }),
        vec![
            AccountMeta::new(relayer_permission, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        &foundation,
    )
    .await;

    let (mirror, _) = get_bond_mirror_pda(&program_id, &bond_ref);
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::WriteBondMirror(BondMirrorWriteArgs {
            bond_ref,
            builder: builder.pubkey(),
            tier: TIER,
            committed_rate_bits_per_sec: TIER.max_rate_bits_per_sec(),
            source_slot: 1,
        }),
        vec![
            AccountMeta::new(mirror, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        &relayer,
        &[AccountMeta::new_readonly(relayer_permission, false)],
    )
    .await;

    let exchange = Pubkey::new_unique();
    let ix = dzi::feed::create_feed(
        &program_id,
        &builder.pubkey(),
        FeedCreateArgs {
            code: "tokyo1".to_string(),
            name: "Tokyo".to_string(),
            exchange,
            groups: vec![group],
            builder: builder.pubkey(),
            bond_ref,
            spec_id: "top-of-book@v1.0.0".to_string(),
            sla_hash,
            committed_rate_bits_per_sec: TIER.max_rate_bits_per_sec(),
            feed_chain: FeedChain::Unspecified,
        },
    );
    process_instructions(&mut banks_client, &[ix], &builder)
        .await
        .expect("the builder's account list should reach the bonded create");

    let (feed, _) = get_feed_pda(&program_id, "tokyo1", &exchange);
    let written = get_account_data(&mut banks_client, feed)
        .await
        .expect("the feed should exist")
        .get_feed()
        .expect("it should be a feed");
    assert_eq!(written.builder, builder.pubkey());
    assert_eq!(written.bond_ref, bond_ref);
    assert_eq!(written.spec_id, "top-of-book@v1.0.0");
    assert_eq!(written.sla_hash, sla_hash);
    assert_eq!(
        written.committed_rate_bits_per_sec,
        TIER.max_rate_bits_per_sec()
    );
    assert_eq!(
        written.status,
        FeedStatus::Active,
        "a bonded feed starts active"
    );

    let claimed = get_account_data(&mut banks_client, mirror)
        .await
        .expect("the mirror should exist")
        .get_bond_mirror()
        .expect("it should be a bond mirror");
    assert_eq!(claimed.feed_key, feed, "the feed spent the bond");
}
