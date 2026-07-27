use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::{set::SetAccessPassArgs, set_flags::SetAccessPassFlagsArgs},
        globalstate::{setauthority::SetAuthorityArgs, setfeatureflags::SetFeatureFlagsArgs},
        permission::create::PermissionCreateArgs,
    },
    state::{
        accesspass::{AccessPassType, ALLOW_MULTIPLE_IP, DZF_LOCKED},
        accounttype::AccountType,
        feature_flags::FeatureFlag,
        permission::permission_flags,
    },
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signature::Keypair,
    signer::Signer,
    transaction::TransactionError,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

// DoubleZeroError codes (see src/error.rs).
const CODE_NOT_ALLOWED: u32 = 8;
const CODE_INVALID_ARGUMENT: u32 = 65;

fn assert_custom_error(result: Result<(), BanksClientError>, expected: u32, context: &str) {
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(code),
        ))) if code == expected => {}
        _ => panic!("{context}: expected Custom({expected}), got {result:?}"),
    }
}

/// Exercises the DZF-locked flag end to end: the generic SetAccessPassFlags instruction sets and
/// clears the bit, preserves the unrelated ALLOW_MULTIPLE_IP bit, an unrelated SetAccessPass update
/// preserves the DZF_LOCKED bit, an all-None call is rejected, and a signer without
/// ACCESS_PASS_ADMIN is rejected.
#[tokio::test]
async fn test_accesspass_dzf_locked() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // Global init: this makes `payer` the sole foundation_allowlist member, which satisfies the
    // ACCESS_PASS_ADMIN legacy fallback.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create a prepaid access pass with allow_multiple_ip set, so we can prove the two flag bits
    // are managed independently.
    let (accesspass_pubkey, _) =
        get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer.pubkey());
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            last_access_epoch: 9999,
            allow_multiple_ip: true,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    let ap = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("access pass")
        .get_accesspass()
        .unwrap();
    assert_eq!(ap.account_type, AccountType::AccessPass);
    assert!(ap.allow_multiple_ip());
    assert!(!ap.dzf_locked());

    // Set dzf_locked (leaving allow_multiple_ip untouched via None).
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPassFlags(SetAccessPassFlagsArgs {
            allow_multiple_ip: None,
            dzf_locked: Some(true),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let ap = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("access pass")
        .get_accesspass()
        .unwrap();
    assert!(ap.dzf_locked());
    assert!(
        ap.allow_multiple_ip(),
        "allow_multiple_ip must be preserved"
    );
    assert_eq!(ap.flags, ALLOW_MULTIPLE_IP | DZF_LOCKED);

    // An unrelated SetAccessPass update (allow_multiple_ip=false) must clear allow_multiple_ip but
    // preserve the DZF_LOCKED bit.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    let ap = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("access pass")
        .get_accesspass()
        .unwrap();
    assert!(!ap.allow_multiple_ip());
    assert!(ap.dzf_locked(), "DZF_LOCKED must survive an unrelated set");
    assert_eq!(ap.flags, DZF_LOCKED);

    // Clear dzf_locked.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPassFlags(SetAccessPassFlagsArgs {
            allow_multiple_ip: None,
            dzf_locked: Some(false),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let ap = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("access pass")
        .get_accesspass()
        .unwrap();
    assert!(!ap.dzf_locked());
    assert_eq!(ap.flags, 0);

    // An all-None call changes nothing and is rejected.
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPassFlags(SetAccessPassFlagsArgs {
            allow_multiple_ip: None,
            dzf_locked: None,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert_custom_error(result, CODE_INVALID_ARGUMENT, "all-None SetAccessPassFlags");

    // A signer without ACCESS_PASS_ADMIN (not foundation/sentinel/feed, no Permission) is rejected.
    let outsider = Keypair::new();
    transfer(&mut banks_client, &payer, &outsider.pubkey(), 100_000_000).await;
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPassFlags(SetAccessPassFlagsArgs {
            allow_multiple_ip: None,
            dzf_locked: Some(true),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &outsider,
    )
    .await;
    assert_custom_error(result, CODE_NOT_ALLOWED, "unauthorized dzf_locked");

    // The rejected attempt left the flag clear.
    let ap = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("access pass")
        .get_accesspass()
        .unwrap();
    assert!(!ap.dzf_locked());
}

/// Pins the feed-authority ownership guard in `process_set_access_pass_flags`. The feed authority
/// holds `ACCESS_PASS_ADMIN`, so `authorize()` alone lets it through; only the follow-up
/// `owner != payer` check stops it from clearing `dzf_locked` on a pass it did not create. The
/// negative case is paired with a positive control on a pass the feed authority *does* own, so a
/// regression that broke `authorize()` outright could not masquerade as the guard firing.
#[tokio::test]
async fn test_accesspass_dzf_locked_feed_authority_limited_to_own_passes() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Promote a fresh keypair to feed authority. It is deliberately not in foundation_allowlist,
    // so feed_authority_pk is the only thing granting it ACCESS_PASS_ADMIN.
    let feed = Keypair::new();
    transfer(&mut banks_client, &payer, &feed.pubkey(), 10_000_000_000).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            feed_authority_pk: Some(feed.pubkey()),
            ..Default::default()
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // A pass created by the foundation payer, so `owner == payer` and the feed authority is not
    // its owner.
    let foundation_ip = Ipv4Addr::new(100, 0, 0, 10);
    let (foundation_pass, _) = get_accesspass_pda(&program_id, &foundation_ip, &payer.pubkey());
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: foundation_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(foundation_pass, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // The feed authority passes ACCESS_PASS_ADMIN but does not own this pass, so the ownership
    // guard rejects it.
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPassFlags(SetAccessPassFlagsArgs {
            allow_multiple_ip: None,
            dzf_locked: Some(true),
        }),
        vec![
            AccountMeta::new(foundation_pass, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &feed,
    )
    .await;
    assert_custom_error(
        result,
        CODE_NOT_ALLOWED,
        "feed authority setting dzf_locked on a pass it does not own",
    );

    let ap = get_account_data(&mut banks_client, foundation_pass)
        .await
        .expect("access pass")
        .get_accesspass()
        .unwrap();
    assert_eq!(ap.owner, payer.pubkey());
    assert!(
        !ap.dzf_locked(),
        "rejected attempt must not have set the bit"
    );

    // Positive control: the same signer on a pass it does own succeeds, proving the rejection
    // above came from the ownership guard and not from authorize().
    let feed_ip = Ipv4Addr::new(100, 0, 0, 11);
    let feed_user_payer = Pubkey::new_unique();
    let (feed_pass, _) = get_accesspass_pda(&program_id, &feed_ip, &feed_user_payer);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: feed_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(feed_pass, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(feed_user_payer, false),
        ],
        &feed,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPassFlags(SetAccessPassFlagsArgs {
            allow_multiple_ip: None,
            dzf_locked: Some(true),
        }),
        vec![
            AccountMeta::new(feed_pass, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &feed,
    )
    .await;

    let ap = get_account_data(&mut banks_client, feed_pass)
        .await
        .expect("access pass")
        .get_accesspass()
        .unwrap();
    assert_eq!(ap.owner, feed.pubkey());
    assert!(ap.dzf_locked());
}

/// Proves the Permission grant path for `SetAccessPassFlags` under
/// `FeatureFlag::RequirePermissionAccounts`: a non-foundation holder of an `ACCESS_PASS_ADMIN`
/// Permission account may set `dzf_locked`, and the same signer is denied once the trailing
/// Permission account is omitted (strict mode disables the legacy allowlist fallback).
#[tokio::test]
async fn test_accesspass_dzf_locked_requires_permission_account_in_strict_mode() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create the pass while the legacy path still works; strict mode is enabled afterwards.
    let client_ip = Ipv4Addr::new(100, 0, 0, 20);
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer.pubkey());
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Foundation grants ACCESS_PASS_ADMIN to a keypair that is in no allowlist and is not the
    // feed/sentinel authority, so the Permission account is its only route to authorization.
    let pass_admin = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &pass_admin.pubkey(),
        10_000_000_000,
    )
    .await;
    let (permission_pda, _) = get_permission_pda(&program_id, &pass_admin.pubkey());
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: pass_admin.pubkey(),
            permissions: permission_flags::ACCESS_PASS_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::RequirePermissionAccounts.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // Grant path: the Permission PDA is appended as the trailing account authorize() reads.
    //
    // Note that `pass_admin` is not the feed authority, so the ownership guard in
    // `process_set_access_pass_flags` never fires and this succeeds against a pass owned by
    // `payer`. That is correct today — an ACCESS_PASS_ADMIN holder may target any pass — but it is
    // also the exact shape the oracle takes after the feed-authority -> Permission migration. When
    // that migration lands, revisit this assertion together with the DEPENDENCY comment on that
    // guard: what reads as an expected success here would then be the regression. Tracked in #4092.
    execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPassFlags(SetAccessPassFlagsArgs {
            allow_multiple_ip: None,
            dzf_locked: Some(true),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &pass_admin,
        &[AccountMeta::new_readonly(permission_pda, false)],
    )
    .await;

    let ap = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("access pass")
        .get_accesspass()
        .unwrap();
    assert!(ap.dzf_locked());

    // Same signer, no Permission account: strict mode disables the legacy fallback, so this is
    // denied even though the grant exists on-chain.
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPassFlags(SetAccessPassFlagsArgs {
            allow_multiple_ip: None,
            dzf_locked: Some(false),
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &pass_admin,
    )
    .await;
    assert_custom_error(
        result,
        CODE_NOT_ALLOWED,
        "strict mode without a Permission account",
    );

    let ap = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("access pass")
        .get_accesspass()
        .unwrap();
    assert!(
        ap.dzf_locked(),
        "denied attempt must not have cleared the bit"
    );
}
