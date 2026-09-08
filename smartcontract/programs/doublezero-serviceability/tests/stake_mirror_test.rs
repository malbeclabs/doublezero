//! Integration tests for `WriteStakeMirror` (RFC-28 A6).
//!
//! The instruction that gives the relayer something to call. Everything here is about what the
//! program refuses, because the mirror is an assertion and these checks are all that bound it.

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_permission_pda, get_stake_mirror_pda},
    processors::{
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        permission::create::PermissionCreateArgs, stake_mirror::write::StakeMirrorWriteArgs,
    },
    state::{
        feature_flags::FeatureFlag,
        permission::permission_flags,
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

struct Fixture {
    banks_client: BanksClient,
    program_id: Pubkey,
    payer: Keypair,
    globalstate_pubkey: Pubkey,
    /// A funded key holding `STAKE_ORACLE` and nothing else.
    relayer: Keypair,
}

/// Bring up a cluster with `allow-staked-feeds` set and one key holding `STAKE_ORACLE`.
async fn setup() -> Fixture {
    let program_id = Pubkey::new_unique();
    let relayer = test_payer();

    let (mut banks_client, payer, recent_blockhash) =
        init_test_with_accounts(program_id, &[]).await;
    init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::AllowStakedFeeds.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // The foundation payer grants the relayer STAKE_ORACLE. No legacy GlobalState key maps to
    // this flag, so a Permission account is the only way to hold it.
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
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    Fixture {
        banks_client,
        program_id,
        payer,
        globalstate_pubkey,
        relayer,
    }
}

fn args(stake_ref: Pubkey, builder: Pubkey, tier: StakeTier, slot: u64) -> StakeMirrorWriteArgs {
    StakeMirrorWriteArgs {
        stake_ref,
        builder,
        tier,
        committed_rate_bits_per_sec: ONE_GBPS,
        source_slot: slot,
    }
}

/// Accounts for the instruction. The harness appends payer and system_program, and the relayer's
/// Permission account rides after those.
fn accounts(mirror: Pubkey, globalstate: Pubkey) -> Vec<AccountMeta> {
    vec![
        AccountMeta::new(mirror, false),
        AccountMeta::new(globalstate, false),
    ]
}

async fn get_mirror(banks_client: &mut BanksClient, key: Pubkey) -> StakeMirror {
    get_account_data(banks_client, key)
        .await
        .expect("Unable to get StakeMirror")
        .get_stake_mirror()
        .unwrap()
}

/// The relayer creates a mirror, then updates it at a newer slot.
#[tokio::test]
async fn test_relayer_writes_and_updates_a_mirror() {
    let mut f = setup().await;
    let stake_ref = Pubkey::new_unique();
    let builder = Pubkey::new_unique();
    let (mirror_pubkey, bump) = get_stake_mirror_pda(&f.program_id, &stake_ref);
    let (relayer_permission, _) = get_permission_pda(&f.program_id, &f.relayer.pubkey());

    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut f.banks_client,
        recent_blockhash,
        f.program_id,
        DoubleZeroInstruction::WriteStakeMirror(args(stake_ref, builder, StakeTier::UpTo1Gbps, 10)),
        accounts(mirror_pubkey, f.globalstate_pubkey),
        &f.relayer,
        &[AccountMeta::new_readonly(relayer_permission, false)],
    )
    .await;

    let mirror = get_mirror(&mut f.banks_client, mirror_pubkey).await;
    assert_eq!(mirror.stake_ref, stake_ref);
    assert_eq!(mirror.builder, builder);
    assert_eq!(mirror.tier, StakeTier::UpTo1Gbps);
    assert_eq!(mirror.source_slot, 10);
    assert_eq!(mirror.bump_seed, bump);
    assert_eq!(
        mirror.relayer,
        f.relayer.pubkey(),
        "the mirror records who vouched for it"
    );
    assert_eq!(mirror.feed_key, Pubkey::default(), "no feed has claimed it");

    // A newer slot raises the tier.
    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut f.banks_client,
        recent_blockhash,
        f.program_id,
        DoubleZeroInstruction::WriteStakeMirror(args(stake_ref, builder, StakeTier::Unmetered, 11)),
        accounts(mirror_pubkey, f.globalstate_pubkey),
        &f.relayer,
        &[AccountMeta::new_readonly(relayer_permission, false)],
    )
    .await;

    let mirror = get_mirror(&mut f.banks_client, mirror_pubkey).await;
    assert_eq!(mirror.tier, StakeTier::Unmetered);
    assert_eq!(mirror.source_slot, 11);
}

/// A write carrying a slot no newer than the stored one is refused.
///
/// Polling makes a repeated write harmless but says nothing about ordering: a retry carrying an
/// older read can arrive after a fresher one. The program enforces this, not the relayer.
#[tokio::test]
async fn test_a_stale_slot_cannot_walk_the_mirror_backwards() {
    let mut f = setup().await;
    let stake_ref = Pubkey::new_unique();
    let builder = Pubkey::new_unique();
    let (mirror_pubkey, _) = get_stake_mirror_pda(&f.program_id, &stake_ref);
    let (relayer_permission, _) = get_permission_pda(&f.program_id, &f.relayer.pubkey());
    let extra = [AccountMeta::new_readonly(relayer_permission, false)];

    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut f.banks_client,
        recent_blockhash,
        f.program_id,
        DoubleZeroInstruction::WriteStakeMirror(args(stake_ref, builder, StakeTier::UpTo5Gbps, 20)),
        accounts(mirror_pubkey, f.globalstate_pubkey),
        &f.relayer,
        &extra,
    )
    .await;

    for stale in [19u64, 20] {
        let result = try_execute_and_get_error(
            &mut f.banks_client,
            f.program_id,
            DoubleZeroInstruction::WriteStakeMirror(args(
                stake_ref,
                builder,
                StakeTier::UpTo1Gbps,
                stale,
            )),
            accounts(mirror_pubkey, f.globalstate_pubkey),
            &f.relayer,
            &extra,
        )
        .await;
        assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
    }

    let mirror = get_mirror(&mut f.banks_client, mirror_pubkey).await;
    assert_eq!(mirror.tier, StakeTier::UpTo5Gbps, "the tier did not move");
    assert_eq!(mirror.source_slot, 20);
}

/// A write must not reassign a mirror to a different builder. The builder is not a PDA seed, so
/// nothing else stops it, and a funded stake could otherwise be handed to another key.
#[tokio::test]
async fn test_a_mirror_cannot_change_builder() {
    let mut f = setup().await;
    let stake_ref = Pubkey::new_unique();
    let builder = Pubkey::new_unique();
    let (mirror_pubkey, _) = get_stake_mirror_pda(&f.program_id, &stake_ref);
    let (relayer_permission, _) = get_permission_pda(&f.program_id, &f.relayer.pubkey());
    let extra = [AccountMeta::new_readonly(relayer_permission, false)];

    let recent_blockhash = wait_for_new_blockhash(&mut f.banks_client).await;
    execute_transaction_with_extra_accounts(
        &mut f.banks_client,
        recent_blockhash,
        f.program_id,
        DoubleZeroInstruction::WriteStakeMirror(args(stake_ref, builder, StakeTier::UpTo1Gbps, 30)),
        accounts(mirror_pubkey, f.globalstate_pubkey),
        &f.relayer,
        &extra,
    )
    .await;

    let result = try_execute_and_get_error(
        &mut f.banks_client,
        f.program_id,
        DoubleZeroInstruction::WriteStakeMirror(args(
            stake_ref,
            Pubkey::new_unique(),
            StakeTier::UpTo1Gbps,
            31,
        )),
        accounts(mirror_pubkey, f.globalstate_pubkey),
        &f.relayer,
        &extra,
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));

    assert_eq!(
        get_mirror(&mut f.banks_client, mirror_pubkey).await.builder,
        builder
    );
}

/// A caller without `STAKE_ORACLE` cannot write a mirror, even the foundation payer that granted
/// the flag in the first place.
#[tokio::test]
async fn test_only_a_stake_oracle_can_write() {
    let mut f = setup().await;
    let stake_ref = Pubkey::new_unique();
    let (mirror_pubkey, _) = get_stake_mirror_pda(&f.program_id, &stake_ref);

    let result = try_execute_and_get_error(
        &mut f.banks_client,
        f.program_id,
        DoubleZeroInstruction::WriteStakeMirror(args(
            stake_ref,
            Pubkey::new_unique(),
            StakeTier::UpTo1Gbps,
            40,
        )),
        accounts(mirror_pubkey, f.globalstate_pubkey),
        &f.payer,
        &[],
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
}

/// The mirror ships dormant with the rest of RFC-28: with the flag clear, even a `STAKE_ORACLE`
/// holder is refused.
#[tokio::test]
async fn test_writing_is_refused_while_the_flag_is_clear() {
    let program_id = Pubkey::new_unique();
    let relayer = test_payer();
    let (mut banks_client, payer, recent_blockhash) =
        init_test_with_accounts(program_id, &[]).await;
    init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

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
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let stake_ref = Pubkey::new_unique();
    let (mirror_pubkey, _) = get_stake_mirror_pda(&program_id, &stake_ref);
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::WriteStakeMirror(args(
            stake_ref,
            Pubkey::new_unique(),
            StakeTier::UpTo1Gbps,
            50,
        )),
        accounts(mirror_pubkey, globalstate_pubkey),
        &relayer,
        &[AccountMeta::new_readonly(relayer_permission, false)],
    )
    .await;
    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
}
