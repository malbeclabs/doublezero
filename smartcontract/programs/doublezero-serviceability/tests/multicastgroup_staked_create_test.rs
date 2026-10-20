//! `CreateMulticastGroup` admitted by a builder's stake rather than by `MULTICAST_ADMIN`.
//!
//! Every mirror here comes from a real `WriteStakeMirror`, and the builder holds no permission at
//! any point. The admin path is covered by `multicastgroup_onchain_allocation_test` and is
//! unchanged.
//!
//! The last tests cover what the owner of a staked group may do with the allowlists. It may grant
//! its own publishers, and no more.

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_globalstate_pda, get_index_pda, get_multicastgroup_pda,
        get_permission_pda, get_resource_extension_pda, get_stake_mirror_pda,
    },
    processors::{
        accesspass::set::SetAccessPassArgs,
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        index::create::IndexCreateArgs,
        multicastgroup::{
            allowlist::{
                publisher::add::AddMulticastGroupPubAllowlistArgs,
                subscriber::add::AddMulticastGroupSubAllowlistArgs,
            },
            create::{MulticastGroupCreateArgs, MAX_GROUPS_PER_STAKE},
        },
        permission::create::PermissionCreateArgs,
        stake_mirror::write::StakeMirrorWriteArgs,
    },
    resource::ResourceType,
    seeds::SEED_MULTICAST_GROUP,
    state::{
        accesspass::AccessPassType,
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
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use std::net::Ipv4Addr;

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
    cluster_with_accounts(Pubkey::new_unique(), &[]).await
}

/// `cluster`, with `accounts` planted at genesis.
async fn cluster_with_accounts(program_id: Pubkey, accounts: &[(Pubkey, Vec<u8>)]) -> Cluster {
    let relayer = Keypair::new();
    let (mut banks_client, foundation, recent_blockhash) =
        init_test_with_accounts(program_id, accounts).await;
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
        self.mirror_at_rate(
            stake_ref,
            builder,
            source_slot,
            TIER.max_rate_bits_per_sec(),
        )
        .await
    }

    /// `mirror`, for a bond whose committed rate is `committed_rate` within `TIER`.
    async fn mirror_at_rate(
        &mut self,
        stake_ref: Pubkey,
        builder: Pubkey,
        source_slot: u64,
        committed_rate: u64,
    ) -> Pubkey {
        self.mirror_at_tier(stake_ref, builder, source_slot, TIER, committed_rate)
            .await
    }

    /// `mirror`, at any tier, including `None` for a stake the relayer saw withdrawn.
    async fn mirror_at_tier(
        &mut self,
        stake_ref: Pubkey,
        builder: Pubkey,
        source_slot: u64,
        tier: StakeTier,
        committed_rate: u64,
    ) -> Pubkey {
        let (mirror, _) = get_stake_mirror_pda(&self.program_id, &stake_ref);
        let recent_blockhash = wait_for_new_blockhash(&mut self.banks_client).await;
        execute_transaction_with_extra_accounts(
            &mut self.banks_client,
            recent_blockhash,
            self.program_id,
            DoubleZeroInstruction::WriteStakeMirror(StakeMirrorWriteArgs {
                stake_ref,
                builder,
                tier,
                committed_rate_bits_per_sec: committed_rate,
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
        let index = self.index_key(&args.code);
        self.create_group_with_tail(
            payer,
            args,
            &[
                AccountMeta::new(mirror, false),
                AccountMeta::new(index, false),
            ],
        )
        .await
    }

    fn index_key(&self, code: &str) -> Pubkey {
        get_index_pda(&self.program_id, SEED_MULTICAST_GROUP, code).0
    }

    /// `create_group`, with `tail` after the fixed accounts in place of the mirror.
    async fn create_group_with_tail(
        &mut self,
        payer: &Keypair,
        args: MulticastGroupCreateArgs,
        tail: &[AccountMeta],
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
            tail,
        )
        .await;
        (group, result)
    }

    /// A group the builder created on a fresh stake of its own.
    async fn staked_group(&mut self, builder: &Keypair, code: &str) -> Pubkey {
        let stake_ref = Pubkey::new_unique();
        let mirror = self.mirror(stake_ref, builder.pubkey(), 1).await;
        let (group, result) = self
            .create_group(
                builder,
                staked_args(code, builder.pubkey(), stake_ref),
                mirror,
            )
            .await;
        result.expect("the staked create should succeed");
        group
    }

    /// Run `AddMulticastGroupPubAllowlist` or `AddMulticastGroupSubAllowlist` for the pass at
    /// `client_ip` and `user_payer`, signed by `signer`.
    async fn allow(
        &mut self,
        signer: &Keypair,
        publisher: bool,
        group: Pubkey,
        client_ip: Ipv4Addr,
        user_payer: Pubkey,
    ) -> Result<(), TransactionError> {
        let (accesspass, _) = get_accesspass_pda(&self.program_id, &client_ip, &user_payer);
        let instruction = if publisher {
            DoubleZeroInstruction::AddMulticastGroupPubAllowlist(
                AddMulticastGroupPubAllowlistArgs {
                    client_ip,
                    user_payer,
                },
            )
        } else {
            DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                AddMulticastGroupSubAllowlistArgs {
                    client_ip,
                    user_payer,
                },
            )
        };
        try_execute_and_get_error(
            &mut self.banks_client,
            self.program_id,
            instruction,
            vec![
                AccountMeta::new(group, false),
                AccountMeta::new(accesspass, false),
                AccountMeta::new(self.globalstate, false),
                AccountMeta::new(user_payer, false),
            ],
            signer,
            &[],
        )
        .await
    }

    async fn pass_lists(
        &mut self,
        client_ip: Ipv4Addr,
        user_payer: Pubkey,
    ) -> (Vec<Pubkey>, Vec<Pubkey>) {
        let (accesspass, _) = get_accesspass_pda(&self.program_id, &client_ip, &user_payer);
        let pass = get_account_data(&mut self.banks_client, accesspass)
            .await
            .expect("the pass should exist")
            .get_accesspass()
            .unwrap();
        (pass.mgroup_pub_allowlist, pass.mgroup_sub_allowlist)
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
                stake_ref: args.stake_ref,
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

        let index_key = self.index_key(&args.code);
        let index = get_account_data(&mut self.banks_client, index_key)
            .await
            .expect("the group's code should be claimed in the Index")
            .get_index()
            .unwrap();
        assert_eq!(
            index.pk, group,
            "the Index entry should point at the new group"
        );
        assert_eq!(index.entity_account_type, AccountType::MulticastGroup);
        assert_eq!(index.key, args.code.to_ascii_lowercase());
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

/// A group may declare up to the bond's tier, as a feed may, even when the builder committed to a
/// lower rate inside that tier. The bond is priced by tier, so the tier is what it covers.
#[tokio::test]
async fn test_a_staked_group_may_use_its_whole_tier() {
    let mut c = cluster().await;
    let builder = test_payer();
    let stake_ref = Pubkey::new_unique();
    // Half the tier, so a check against the committed rate would refuse the create below.
    let committed_rate = TIER.max_rate_bits_per_sec() / 2;
    let mirror = c
        .mirror_at_rate(stake_ref, builder.pubkey(), 1, committed_rate)
        .await;

    let args = staked_args("whole-tier", builder.pubkey(), stake_ref);
    assert_eq!(args.max_bandwidth, TIER.max_rate_bits_per_sec());
    let mirror_before = c.stake_mirror(mirror).await;
    let (group, result) = c.create_group(&builder, args.clone(), mirror).await;
    result.expect("a group within the tier should be admitted, whatever the committed rate");
    c.assert_created(group, &args, mirror, mirror_before).await;
}

/// A mirror written before `groups_created` existed is one byte short. The first staked create
/// reads its count as zero and grows the account to hold it. Live testnet mirrors take this path.
#[tokio::test]
async fn test_a_mirror_from_before_the_count_takes_its_first_group() {
    let program_id = Pubkey::new_unique();
    let builder = test_payer();
    let stake_ref = Pubkey::new_unique();
    let (mirror, bump_seed) = get_stake_mirror_pda(&program_id, &stake_ref);
    let old = StakeMirror {
        account_type: AccountType::StakeMirror,
        owner: Pubkey::new_unique(),
        bump_seed,
        stake_ref,
        builder: builder.pubkey(),
        tier: TIER,
        committed_rate_bits_per_sec: TIER.max_rate_bits_per_sec(),
        source_slot: 1,
        relayer: Pubkey::new_unique(),
        feed_key: Pubkey::default(),
        groups_created: 0,
    };
    let mut data = borsh::to_vec(&old).unwrap();
    // `groups_created` is the last field and one byte wide. Dropping it gives the old layout.
    data.pop();
    let old_len = data.len();

    let mut c = cluster_with_accounts(program_id, &[(mirror, data)]).await;
    let args = staked_args("old-layout", builder.pubkey(), stake_ref);
    let (group, result) = c.create_group(&builder, args.clone(), mirror).await;
    result.expect("an old-layout mirror should admit the create");
    c.assert_created(group, &args, mirror, old).await;

    let account = c.banks_client.get_account(mirror).await.unwrap().unwrap();
    assert_eq!(
        account.data.len(),
        old_len + 1,
        "the mirror grew to hold the count"
    );
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

    // Without the mirror the answer is the same. A cluster with the flag clear must not tell the
    // caller to attach a mirror that cannot help it.
    let (_, result) = c
        .create_group_with_tail(
            &builder,
            staked_args("flag-clear-no-mirror", builder.pubkey(), stake_ref),
            &[],
        )
        .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
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

/// The owner of a staked group grants its own publisher, on a pass the grant creates, and again
/// on that pass once it exists. This is how a builder sends on its feed (A4).
#[tokio::test]
async fn test_a_staked_groups_owner_grants_its_own_publishers() {
    let mut c = cluster().await;
    let builder = test_payer();
    let group = c.staked_group(&builder, "own-pub").await;
    let client_ip = Ipv4Addr::new(10, 0, 0, 7);

    c.allow(&builder, true, group, client_ip, builder.pubkey())
        .await
        .expect("the owner should grant a publisher on a new pass");
    c.allow(&builder, true, group, client_ip, builder.pubkey())
        .await
        .expect("the owner should grant again on a pass it owns");

    let (publishers, subscribers) = c.pass_lists(client_ip, builder.pubkey()).await;
    assert_eq!(publishers, vec![group]);
    assert!(subscribers.is_empty());
}

/// The owner of a staked group cannot put it on another user's pass. An admin still can.
#[tokio::test]
async fn test_a_staked_groups_owner_cannot_reach_another_users_pass() {
    let mut c = cluster().await;
    let builder = test_payer();
    let group = c.staked_group(&builder, "reach").await;

    // A pass the foundation set up for someone else.
    let user = Pubkey::new_unique();
    let client_ip = Ipv4Addr::new(10, 0, 0, 8);
    let (accesspass, _) = get_accesspass_pda(&c.program_id, &client_ip, &user);
    let recent_blockhash = wait_for_new_blockhash(&mut c.banks_client).await;
    execute_transaction(
        &mut c.banks_client,
        recent_blockhash,
        c.program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 100,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass, false),
            AccountMeta::new(c.globalstate, false),
            AccountMeta::new(user, false),
        ],
        &c.foundation,
    )
    .await;

    let result = c.allow(&builder, true, group, client_ip, user).await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
    assert!(c.pass_lists(client_ip, user).await.0.is_empty());

    let foundation = c.foundation.insecure_clone();
    c.allow(&foundation, true, group, client_ip, user)
        .await
        .expect("an admin keeps its reach over a staked group");
    assert_eq!(c.pass_lists(client_ip, user).await.0, vec![group]);
}

/// The owner of a staked group cannot create a pass for another user. That pass would be the
/// builder's, and it would block the user's real pass and later pass the owner check.
#[tokio::test]
async fn test_a_staked_groups_owner_cannot_create_a_pass_for_another_user() {
    let mut c = cluster().await;
    let builder = test_payer();
    let group = c.staked_group(&builder, "squat").await;
    let user = Pubkey::new_unique();
    let client_ip = Ipv4Addr::new(10, 0, 0, 10);

    let result = c.allow(&builder, true, group, client_ip, user).await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
    let (accesspass, _) = get_accesspass_pda(&c.program_id, &client_ip, &user);
    assert!(
        c.banks_client
            .get_account(accesspass)
            .await
            .unwrap()
            .is_none(),
        "no pass may be left behind for the user"
    );
}

/// The owner of a staked group cannot admit subscribers. They would join on a Prepaid pass and
/// skip the EdgeSeat check where a feed sells its seats. An admin still can.
#[tokio::test]
async fn test_a_staked_groups_owner_cannot_admit_subscribers() {
    let mut c = cluster().await;
    let builder = test_payer();
    let group = c.staked_group(&builder, "no-sub").await;
    let user = Pubkey::new_unique();
    let client_ip = Ipv4Addr::new(10, 0, 0, 9);

    let result = c.allow(&builder, false, group, client_ip, user).await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));

    let foundation = c.foundation.insecure_clone();
    c.allow(&foundation, false, group, client_ip, user)
        .await
        .expect("an admin may still admit a subscriber");
    assert_eq!(c.pass_lists(client_ip, user).await.1, vec![group]);
}

/// A stake the relayer rewrote to tier `None`, because the bond was withdrawn or forfeited, admits
/// no create, though the mirror still names the builder.
#[tokio::test]
async fn test_a_withdrawn_stake_admits_nothing() {
    let mut c = cluster().await;
    let builder = test_payer();
    let stake_ref = Pubkey::new_unique();
    c.mirror(stake_ref, builder.pubkey(), 1).await;
    let mirror = c
        .mirror_at_tier(stake_ref, builder.pubkey(), 2, StakeTier::None, 0)
        .await;
    assert_eq!(c.stake_mirror(mirror).await.tier, StakeTier::None);

    let (_, result) = c
        .create_group(
            &builder,
            staked_args("withdrawn", builder.pubkey(), stake_ref),
            mirror,
        )
        .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::StakeDoesNotCoverRate));
    assert_eq!(c.groups_created(mirror).await, 0);
}

/// With `require-permission-accounts` on, a builder that holds no Permission account still creates
/// groups on its stake, because the stake path never calls `authorize`. The SDK sends the payer's
/// Permission PDA after the mirror, never created, and the stake path ignores it.
#[tokio::test]
async fn test_the_stake_path_needs_no_permission_in_strict_mode() {
    let mut c = cluster().await;
    let builder = test_payer();
    let stake_ref = Pubkey::new_unique();
    let mirror = c.mirror(stake_ref, builder.pubkey(), 1).await;

    // Turned on after setup: strict mode refuses the foundation's legacy path, which the relayer's
    // permission and the mirror above were written through.
    let recent_blockhash = wait_for_new_blockhash(&mut c.banks_client).await;
    execute_transaction(
        &mut c.banks_client,
        recent_blockhash,
        c.program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::AllowStakedFeeds.to_mask()
                | FeatureFlag::RequirePermissionAccounts.to_mask(),
        }),
        vec![AccountMeta::new(c.globalstate, false)],
        &c.foundation,
    )
    .await;

    let (permission, _) = get_permission_pda(&c.program_id, &builder.pubkey());
    let args = staked_args("strict", builder.pubkey(), stake_ref);
    let mirror_before = c.stake_mirror(mirror).await;
    let (group, result) = c
        .create_group_with_tail(
            &builder,
            args.clone(),
            &[
                AccountMeta::new(mirror, false),
                AccountMeta::new(c.index_key("strict"), false),
                AccountMeta::new_readonly(permission, false),
            ],
        )
        .await;
    result.expect("the stake path should not need a Permission account");
    c.assert_created(group, &args, mirror, mirror_before).await;
}

fn assert_already_initialized(result: &Result<(), TransactionError>) {
    assert_eq!(
        result,
        &Err(TransactionError::InstructionError(
            0,
            InstructionError::AccountAlreadyInitialized
        )),
        "a taken code should fail the create"
    );
}

/// Two staked groups cannot share a code, whichever builders and stakes create them. The Index
/// compares codes without case, as clients resolve them.
#[tokio::test]
async fn test_a_staked_group_cannot_reuse_a_code() {
    let mut c = cluster().await;
    let builder = test_payer();
    c.staked_group(&builder, "tokyo-tob").await;

    let stake_ref = Pubkey::new_unique();
    let mirror = c.mirror(stake_ref, builder.pubkey(), 1).await;
    let (_, result) = c
        .create_group(
            &builder,
            staked_args("TOKYO-tob", builder.pubkey(), stake_ref),
            mirror,
        )
        .await;
    assert_already_initialized(&result);
    assert_eq!(c.groups_created(mirror).await, 0);
}

/// A staked group cannot take the code of an admin group once that group is in the Index, which
/// is what the operator's backfill puts it in.
#[tokio::test]
async fn test_a_staked_group_cannot_take_an_indexed_admin_code() {
    let mut c = cluster().await;
    let builder = test_payer();

    let index = get_globalstate(&mut c.banks_client, c.globalstate)
        .await
        .account_index
        + 1;
    let (admin_group, _) = get_multicastgroup_pda(&c.program_id, index);
    let foundation = c.foundation.insecure_clone();
    c.create_group_with_tail(
        &foundation,
        MulticastGroupCreateArgs {
            code: "admin-mg".to_string(),
            max_bandwidth: 1_000,
            owner: foundation.pubkey(),
            use_onchain_allocation: true,
            stake_ref: Pubkey::default(),
        },
        &[],
    )
    .await
    .1
    .expect("the admin create should succeed");
    let admin_index = c.index_key("admin-mg");
    let recent_blockhash = wait_for_new_blockhash(&mut c.banks_client).await;
    execute_transaction(
        &mut c.banks_client,
        recent_blockhash,
        c.program_id,
        DoubleZeroInstruction::CreateIndex(IndexCreateArgs {
            entity_seed: String::from_utf8(SEED_MULTICAST_GROUP.to_vec()).unwrap(),
            key: "admin-mg".to_string(),
        }),
        vec![
            AccountMeta::new(admin_index, false),
            AccountMeta::new_readonly(admin_group, false),
            AccountMeta::new_readonly(c.globalstate, false),
        ],
        &foundation,
    )
    .await;

    let stake_ref = Pubkey::new_unique();
    let mirror = c.mirror(stake_ref, builder.pubkey(), 1).await;
    let (_, result) = c
        .create_group(
            &builder,
            staked_args("admin-mg", builder.pubkey(), stake_ref),
            mirror,
        )
        .await;
    assert_already_initialized(&result);
}

/// A staked create without its code's Index account is refused, so no staked group can skip the
/// uniqueness check.
#[tokio::test]
async fn test_a_staked_group_must_claim_its_code() {
    let mut c = cluster().await;
    let builder = test_payer();
    let stake_ref = Pubkey::new_unique();
    let mirror = c.mirror(stake_ref, builder.pubkey(), 1).await;

    let (_, result) = c
        .create_group_with_tail(
            &builder,
            staked_args("no-index", builder.pubkey(), stake_ref),
            &[AccountMeta::new(mirror, false)],
        )
        .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
    assert_eq!(c.groups_created(mirror).await, 0);
}
