//! `CreateMulticastGroup` admitted by a builder's stake rather than by `MULTICAST_ADMIN`.
//!
//! Every mirror here comes from a real `WriteStakeMirror`, and the builder holds no permission at
//! any point. The admin path is covered by `multicastgroup_onchain_allocation_test` and is
//! unchanged.

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{
        get_globalstate_pda, get_multicastgroup_pda, get_permission_pda,
        get_resource_extension_pda, get_stake_mirror_pda,
    },
    processors::{
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        multicastgroup::create::{MulticastGroupCreateArgs, MAX_GROUPS_PER_STAKE},
        permission::create::PermissionCreateArgs,
        stake_mirror::write::StakeMirrorWriteArgs,
    },
    resource::ResourceType,
    state::{
        accounttype::AccountType,
        feature_flags::FeatureFlag,
        multicastgroup::{MulticastGroup, MulticastGroupStatus},
        permission::permission_flags,
        stake_mirror::{StakeMirror, StakeTier},
    },
};
use doublezero_serviceability_instruction as dzi;
use solana_program_test::*;
use solana_sdk::{
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

const TIER: StakeTier = StakeTier::UpTo1Gbps;

struct Cluster {
    banks_client: BanksClient,
    program_id: Pubkey,
    foundation: Keypair,
    relayer: Keypair,
    relayer_permission: Pubkey,
    globalstate: Pubkey,
}

/// A cluster with `allow-staked-feeds` set and a relayer allowed to write mirrors.
async fn cluster() -> Cluster {
    let program_id = Pubkey::new_unique();
    let relayer = Keypair::new();
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

    Cluster {
        banks_client,
        program_id,
        foundation,
        relayer,
        relayer_permission,
        globalstate,
    }
}

impl Cluster {
    /// Mirror a stake for `builder`, as the relayer would after seeing the bond at `source_slot`.
    async fn mirror(&mut self, stake_ref: Pubkey, builder: Pubkey, source_slot: u64) -> Pubkey {
        let (mirror, _) = get_stake_mirror_pda(&self.program_id, &stake_ref);
        let recent_blockhash = wait_for_new_blockhash(&mut self.banks_client).await;
        execute_transaction_with_extra_accounts(
            &mut self.banks_client,
            recent_blockhash,
            self.program_id,
            DoubleZeroInstruction::WriteStakeMirror(StakeMirrorWriteArgs {
                stake_ref,
                builder,
                tier: TIER,
                committed_rate_bits_per_sec: TIER.max_rate_bits_per_sec(),
                source_slot,
            }),
            vec![
                AccountMeta::new(mirror, false),
                AccountMeta::new_readonly(self.globalstate, false),
            ],
            &self.relayer,
            &[AccountMeta::new_readonly(self.relayer_permission, false)],
        )
        .await;
        mirror
    }

    /// Create a group signed by `payer` on the stake `stake_ref`, with `mirror` in the tail.
    async fn create_group(
        &mut self,
        payer: &Keypair,
        args: MulticastGroupCreateArgs,
        mirror: Pubkey,
    ) -> (Pubkey, Result<(), TransactionError>) {
        let index = get_globalstate(&mut self.banks_client, self.globalstate)
            .await
            .account_index
            + 1;
        let (group, _) = get_multicastgroup_pda(&self.program_id, index);
        let result = try_execute_and_get_error(
            &mut self.banks_client,
            self.program_id,
            DoubleZeroInstruction::CreateMulticastGroup(args),
            vec![
                AccountMeta::new(group, false),
                AccountMeta::new(self.globalstate, false),
                AccountMeta::new(
                    get_resource_extension_pda(&self.program_id, ResourceType::MulticastGroupBlock)
                        .0,
                    false,
                ),
            ],
            payer,
            &[AccountMeta::new(mirror, false)],
        )
        .await;
        (group, result)
    }

    async fn stake_mirror(&mut self, mirror: Pubkey) -> StakeMirror {
        get_account_data(&mut self.banks_client, mirror)
            .await
            .expect("the mirror should exist")
            .get_stake_mirror()
            .unwrap()
    }

    async fn groups_created(&mut self, mirror: Pubkey) -> u8 {
        self.stake_mirror(mirror).await.groups_created
    }

    /// Assert that a staked create wrote exactly the group `args` describe, and changed nothing on
    /// the mirror but its count.
    async fn assert_created(
        &mut self,
        group: Pubkey,
        args: &MulticastGroupCreateArgs,
        mirror: Pubkey,
        mirror_before: StakeMirror,
    ) {
        let written = get_account_data(&mut self.banks_client, group)
            .await
            .expect("the group should exist")
            .get_multicastgroup()
            .unwrap();
        let index = get_globalstate(&mut self.banks_client, self.globalstate)
            .await
            .account_index;
        let (expected_key, bump_seed) = get_multicastgroup_pda(&self.program_id, index);
        assert_eq!(group, expected_key);
        assert_eq!(
            written,
            MulticastGroup {
                account_type: AccountType::MulticastGroup,
                owner: args.owner,
                index,
                bump_seed,
                tenant_pk: Pubkey::default(),
                code: args.code.clone(),
                // Allocated from the block, so only the allocator's own tests pin its value.
                multicast_ip: written.multicast_ip,
                max_bandwidth: args.max_bandwidth,
                status: MulticastGroupStatus::Activated,
                publisher_count: 0,
                subscriber_count: 0,
            }
        );

        assert_eq!(
            self.stake_mirror(mirror).await,
            StakeMirror {
                groups_created: mirror_before.groups_created + 1,
                ..mirror_before
            },
            "the create must change the mirror's count and nothing else"
        );
    }
}

fn staked_args(code: &str, owner: Pubkey, stake_ref: Pubkey) -> MulticastGroupCreateArgs {
    MulticastGroupCreateArgs {
        code: code.to_string(),
        max_bandwidth: TIER.max_rate_bits_per_sec(),
        owner,
        use_onchain_allocation: true,
        stake_ref,
    }
}

/// The builder creates groups on its bond up to the limit, and owns each one. A relayer write in
/// the middle does not reset the count.
#[tokio::test]
async fn test_a_builder_creates_its_own_groups_up_to_the_limit() {
    let mut c = cluster().await;
    let builder = test_payer();
    let stake_ref = Pubkey::new_unique();
    let mirror = c.mirror(stake_ref, builder.pubkey(), 1).await;

    for i in 0..MAX_GROUPS_PER_STAKE {
        let args = staked_args(&format!("g{i}"), builder.pubkey(), stake_ref);
        let mirror_before = c.stake_mirror(mirror).await;
        let (group, result) = c.create_group(&builder, args.clone(), mirror).await;
        result.expect("a covering stake should admit the create");
        c.assert_created(group, &args, mirror, mirror_before).await;
        assert_eq!(c.groups_created(mirror).await, i + 1);

        // The relayer rewrites the mirror after the first group. If the write zeroed the count,
        // the limit below would never be reached.
        if i == 0 {
            c.mirror(stake_ref, builder.pubkey(), 2).await;
            assert_eq!(
                c.groups_created(mirror).await,
                1,
                "a relayer write must carry the count forward"
            );
        }
    }

    let (_, result) = c
        .create_group(
            &builder,
            staked_args("one-too-many", builder.pubkey(), stake_ref),
            mirror,
        )
        .await;
    assert_custom_at_ix0(
        &result,
        custom_code(DoubleZeroError::StakeGroupLimitReached),
    );
    assert_eq!(c.groups_created(mirror).await, MAX_GROUPS_PER_STAKE);

    // Every group was admitted by the bond. A permission or a foundation key would make the
    // creates above prove nothing.
    assert!(
        c.banks_client
            .get_account(get_permission_pda(&c.program_id, &builder.pubkey()).0)
            .await
            .unwrap()
            .is_none(),
        "the builder must have created its groups on its bond, not on a permission"
    );
    let globalstate = get_globalstate(&mut c.banks_client, c.globalstate).await;
    assert!(!globalstate.foundation_allowlist.contains(&builder.pubkey()));
}

/// One builder cannot spend another builder's bond by naming its stake.
#[tokio::test]
async fn test_a_stake_admits_only_its_own_builder() {
    let mut c = cluster().await;
    let builder = test_payer();
    let stake_ref = Pubkey::new_unique();
    let mirror = c.mirror(stake_ref, builder.pubkey(), 1).await;

    let stranger = Keypair::new();
    transfer(
        &mut c.banks_client,
        &c.foundation,
        &stranger.pubkey(),
        100_000_000,
    )
    .await;

    let (_, result) = c
        .create_group(
            &stranger,
            staked_args("stolen", stranger.pubkey(), stake_ref),
            mirror,
        )
        .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
    assert_eq!(c.groups_created(mirror).await, 0);
}

/// A bond cannot create a group whose publish rights someone else holds.
#[tokio::test]
async fn test_a_staked_group_must_be_owned_by_its_builder() {
    let mut c = cluster().await;
    let builder = test_payer();
    let stake_ref = Pubkey::new_unique();
    let mirror = c.mirror(stake_ref, builder.pubkey(), 1).await;

    let (_, result) = c
        .create_group(
            &builder,
            staked_args("for-someone-else", Pubkey::new_unique(), stake_ref),
            mirror,
        )
        .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
}

/// A group cannot ask for more bandwidth than the bond's tier covers.
#[tokio::test]
async fn test_a_staked_group_cannot_exceed_its_tier() {
    let mut c = cluster().await;
    let builder = test_payer();
    let stake_ref = Pubkey::new_unique();
    let mirror = c.mirror(stake_ref, builder.pubkey(), 1).await;

    let mut args = staked_args("too-wide", builder.pubkey(), stake_ref);
    args.max_bandwidth = TIER.max_rate_bits_per_sec() + 1;
    let (_, result) = c.create_group(&builder, args, mirror).await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::StakeDoesNotCoverRate));
}

/// Naming a stake nobody posted, with the empty account its seeds derive, admits nothing.
#[tokio::test]
async fn test_an_unwritten_mirror_admits_nothing() {
    let mut c = cluster().await;
    let builder = test_payer();
    let stake_ref = Pubkey::new_unique();
    let (mirror, _) = get_stake_mirror_pda(&c.program_id, &stake_ref);

    let (_, result) = c
        .create_group(
            &builder,
            staked_args("no-bond", builder.pubkey(), stake_ref),
            mirror,
        )
        .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::StakeMirrorMissing));
}

/// Clearing `allow-staked-feeds` stops a stake admitting a create, though its mirror stays.
#[tokio::test]
async fn test_a_stake_admits_nothing_once_the_flag_is_cleared() {
    let mut c = cluster().await;
    let builder = test_payer();
    let stake_ref = Pubkey::new_unique();
    let mirror = c.mirror(stake_ref, builder.pubkey(), 1).await;

    let recent_blockhash = wait_for_new_blockhash(&mut c.banks_client).await;
    execute_transaction(
        &mut c.banks_client,
        recent_blockhash,
        c.program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs { feature_flags: 0 }),
        vec![AccountMeta::new(c.globalstate, false)],
        &c.foundation,
    )
    .await;

    let (_, result) = c
        .create_group(
            &builder,
            staked_args("flag-clear", builder.pubkey(), stake_ref),
            mirror,
        )
        .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
    assert_eq!(c.groups_created(mirror).await, 0);
}

/// The SDK's builder emits an account list the stake path accepts: the mirror after the system
/// program, and the payer's Permission PDA last, never created.
#[tokio::test]
async fn test_the_instruction_builder_reaches_the_stake_path() {
    let mut c = cluster().await;
    let builder = test_payer();
    let stake_ref = Pubkey::new_unique();
    let mirror = c.mirror(stake_ref, builder.pubkey(), 1).await;

    let index = get_globalstate(&mut c.banks_client, c.globalstate)
        .await
        .account_index
        + 1;
    let args = staked_args("via-sdk", builder.pubkey(), stake_ref);
    let ix = dzi::multicastgroup::create_multicast_group(
        &c.program_id,
        &builder.pubkey(),
        index,
        args.clone(),
    );
    let group = ix.accounts[0].pubkey;
    let mirror_before = c.stake_mirror(mirror).await;
    process_instructions(&mut c.banks_client, &[ix], &builder)
        .await
        .expect("the builder's account list should reach the stake path");

    c.assert_created(group, &args, mirror, mirror_before).await;
}
