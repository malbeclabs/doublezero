use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::accesspass::{set::SetAccessPassArgs, set_flags::SetAccessPassFlagsArgs},
    state::{
        accesspass::{AccessPassType, ALLOW_MULTIPLE_IP, DZF_LOCKED},
        accounttype::AccountType,
    },
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
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
