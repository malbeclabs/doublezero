//! The RFC-28 sequence, driven end to end through real instructions.
//!
//! What is new here is the reach: a real `CreateFeed` against a mirror a real `WriteBondMirror`
//! produced, ending on a feed that sells seats. The relayer mirrors a bond, a builder holding no
//! permission creates a feed against it, which starts `Active`, and grants its own publisher the
//! right to send. No operator takes part.
//!
//! Two siblings already chain instructions, so composition on its own is not the novelty:
//! `feed_test::test_builder_grants_its_own_publish_rights` walks create plus two allowlist grants
//! with two signers, and `feed_test::test_a_mirror_update_keeps_the_claiming_feed` drives
//! `CreatePermission` to `WriteBondMirror` to `CreateFeed` without seeding either.
//!
//! What it does not cover, and what still has to be proven elsewhere:
//!
//! - **The bond.** `PostBond` is a different program on Solana, so the mirror here is written from
//!   values rather than read from a `BuilderBond`. The relayer decoding a real bond is C2.
//! - **Datagrams.** Nothing in a `ProgramTest` sends multicast. That is G2, against a cluster.
//! - **The refusals.** Only the path that succeeds runs here. `feed_test` and
//!   `feed_lifecycle_test` hold the guards.

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_bond_mirror_pda, get_feed_pda, get_globalstate_pda,
        get_multicastgroup_pda, get_permission_pda, get_resource_extension_pda,
    },
    processors::{
        bond_mirror::write::BondMirrorWriteArgs,
        feed::create::FeedCreateArgs,
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        multicastgroup::{
            allowlist::publisher::add::AddMulticastGroupPubAllowlistArgs,
            create::MulticastGroupCreateArgs,
        },
        permission::create::PermissionCreateArgs,
    },
    resource::ResourceType,
    state::{
        bond_mirror::BondTier, feature_flags::FeatureFlag, feed::FeedStatus,
        permission::permission_flags,
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

/// The tier the bond buys. Both committed rates come from its own ceiling, so the coverage check
/// runs at the boundary rather than comfortably under it wherever that ceiling moves.
const TIER: BondTier = BondTier::UpTo1Gbps;

/// The whole sequence, in order, with nothing seeded that an instruction could write.
#[tokio::test]
async fn test_a_builder_deploys_a_feed_that_sells_seats_at_once() {
    let program_id = Pubkey::new_unique();

    // The builder holds no permission at any point. That is the claim under test.
    let builder = test_payer();
    let relayer = Keypair::new();
    let bond_ref = Pubkey::new_unique();

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
            feature_flags: FeatureFlag::AllowBondedFeeds.to_mask(),
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
            bond_ref: Pubkey::default(),
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
            permissions: permission_flags::BOND_ORACLE,
        }),
        vec![
            AccountMeta::new(relayer_permission, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        &foundation,
    )
    .await;

    // ---- Relayer: the bond reaches the ledger. -----------------------------------------------
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
            bond_ref,
            spec_id: "top-of-book@v1.0.0".to_string(),
            sla_hash: [9u8; 32],
            committed_rate_bits_per_sec: TIER.max_rate_bits_per_sec(),
            ..Default::default()
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
        FeedStatus::Active,
        "the bond that admitted the create is the admission"
    );

    // The bond is spent on this feed, which is what makes one bond back one feed.
    let claimed = get_account_data(&mut banks_client, mirror)
        .await
        .expect("the mirror should exist")
        .get_bond_mirror()
        .unwrap();
    assert_eq!(claimed.feed_key, feed, "the feed spent the bond");

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
            // The optional `user_payer`, which funds connect credits. Every real caller sends it,
            // so a sequence test that drops it drives a shape production never produces.
            AccountMeta::new(builder.pubkey(), false),
        ],
        &builder,
    )
    .await;

    let pass = get_account_data(&mut banks_client, accesspass)
        .await
        .expect("the access pass should exist")
        .get_accesspass()
        .expect("it should be an access pass");
    assert!(
        pass.mgroup_pub_allowlist.contains(&group),
        "the builder granted itself nothing"
    );
    assert_eq!(pass.user_payer, builder.pubkey());

    // The builder was authorized by its bond and nothing else. If anything else had admitted it,
    // the create above would prove nothing, so every route into `authorize` is ruled out rather
    // than some of them. `CreateFeed` asks for `FEED_AUTHORITY | FOUNDATION`, which three grants
    // satisfy: a `Permission` account, the legacy foundation allowlist, and the legacy feed
    // authority.
    //
    // Read through `banks_client` rather than `get_account_data`, which returns `None` both for an
    // account that is absent and for one that stopped deserializing. This assertion is what makes
    // the rest of the test mean anything, so it must not pass for the second reason.
    assert!(
        banks_client
            .get_account(get_permission_pda(&program_id, &builder.pubkey()).0)
            .await
            .unwrap()
            .is_none(),
        "the builder must have deployed on its bond, not on a permission"
    );
    let final_globalstate = get_globalstate(&mut banks_client, globalstate).await;
    assert!(
        !final_globalstate
            .foundation_allowlist
            .contains(&builder.pubkey()),
        "the builder must not be a foundation key, or the create above proves nothing"
    );
    assert_ne!(
        final_globalstate.feed_authority_pk,
        builder.pubkey(),
        "the builder must not be the feed authority, or the create above proves nothing"
    );
}
