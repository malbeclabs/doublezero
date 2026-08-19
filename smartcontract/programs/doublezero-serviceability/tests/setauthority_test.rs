//! SetAuthority coverage for `ip_verifier_authority_pk` (RFC-27, issue #4196).
//!
//! The verifier public key is the trust root for IP ownership proof validation, so
//! it has to be rotatable onchain without a program upgrade, and rotating it must
//! not disturb the other authorities.

use borsh::to_vec;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::*,
    processors::globalstate::setauthority::SetAuthorityArgs,
    state::{accounttype::AccountType, globalstate::GlobalState},
};
use solana_program_test::*;
use solana_sdk::{
    account::AccountSharedData,
    instruction::AccountMeta,
    pubkey::Pubkey,
    rent::Rent,
    signature::{Keypair, Signer},
};

mod test_helpers;
use test_helpers::*;

/// Bring up a program instance with an initialized GlobalState.
async fn setup() -> (
    BanksClient,
    Pubkey,
    Keypair,
    solana_program::hash::Hash,
    Pubkey,
) {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    (
        banks_client,
        program_id,
        payer,
        recent_blockhash,
        globalstate_pubkey,
    )
}

#[tokio::test]
async fn test_setauthority_rotates_ip_verifier_authority() {
    let (mut banks_client, program_id, payer, recent_blockhash, globalstate_pubkey) = setup().await;

    // A freshly initialized GlobalState has no verifier configured. Enforcement
    // must read this as "reject", never as "any signature passes".
    let before = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(before.ip_verifier_authority_pk, Pubkey::default());

    let verifier = Keypair::new();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            ip_verifier_authority_pk: Some(verifier.pubkey()),
            ..Default::default()
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let after = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(after.ip_verifier_authority_pk, verifier.pubkey());

    // Every other authority is untouched.
    assert_eq!(after.activator_authority_pk, before.activator_authority_pk);
    assert_eq!(after.sentinel_authority_pk, before.sentinel_authority_pk);
    assert_eq!(after.health_oracle_pk, before.health_oracle_pk);
    assert_eq!(after.feed_authority_pk, before.feed_authority_pk);
    assert_eq!(after.foundation_allowlist, before.foundation_allowlist);
    assert_eq!(after.feature_flags, before.feature_flags);

    // Rotating again replaces the previous key.
    let rotated = Keypair::new();
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            ip_verifier_authority_pk: Some(rotated.pubkey()),
            ..Default::default()
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let after = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(after.ip_verifier_authority_pk, rotated.pubkey());
}

#[tokio::test]
async fn test_setauthority_none_leaves_ip_verifier_authority_unchanged() {
    let (mut banks_client, program_id, payer, recent_blockhash, globalstate_pubkey) = setup().await;

    let verifier = Keypair::new();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            ip_verifier_authority_pk: Some(verifier.pubkey()),
            ..Default::default()
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // A later SetAuthority that only touches another authority must not clear the
    // verifier key.
    let sentinel = Keypair::new();
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            sentinel_authority_pk: Some(sentinel.pubkey()),
            ip_verifier_authority_pk: None,
            ..Default::default()
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let after = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(after.sentinel_authority_pk, sentinel.pubkey());
    assert_eq!(after.ip_verifier_authority_pk, verifier.pubkey());
}

/// The first post-upgrade `SetAuthority` on mainnet writes a `GlobalState` account
/// that was allocated before `ip_verifier_authority_pk` existed, so the write has to
/// grow the account by 32 bytes and top up its rent. Seed exactly that shape — a
/// legacy-sized account funded only to the legacy minimum balance — and rotate.
#[tokio::test]
async fn test_setauthority_grows_legacy_sized_globalstate() {
    let (mut context, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig_context().await;
    let payer = context.payer.insecure_clone();
    let recent_blockhash = context.last_blockhash;

    // Truncate the current account to the pre-field layout: everything up to but not
    // including the trailing 32-byte verifier key.
    let mut account = context
        .banks_client
        .get_account(globalstate_pubkey)
        .await
        .unwrap()
        .expect("GlobalState account should exist");
    let current = GlobalState::try_from(&account.data[..]).unwrap();
    assert_eq!(current.ip_verifier_authority_pk, Pubkey::default());

    let mut legacy_data = to_vec(&current).unwrap();
    legacy_data.truncate(legacy_data.len() - 32);
    let legacy_len = legacy_data.len();

    // A legacy account still deserializes, with the field defaulted.
    let legacy_state = GlobalState::try_from(&legacy_data[..]).unwrap();
    assert_eq!(legacy_state.account_type, AccountType::GlobalState);
    assert_eq!(legacy_state.ip_verifier_authority_pk, Pubkey::default());

    // Fund it to exactly the legacy minimum balance so the grow needs a top-up.
    let legacy_lamports = Rent::default().minimum_balance(legacy_len);
    account.data = legacy_data;
    account.lamports = legacy_lamports;
    context.set_account(&globalstate_pubkey, &AccountSharedData::from(account));

    let verifier = Keypair::new();
    execute_transaction(
        &mut context.banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            ip_verifier_authority_pk: Some(verifier.pubkey()),
            ..Default::default()
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let grown = context
        .banks_client
        .get_account(globalstate_pubkey)
        .await
        .unwrap()
        .expect("GlobalState account should exist");
    assert_eq!(grown.data.len(), legacy_len + 32);
    assert!(
        grown.lamports >= Rent::default().minimum_balance(grown.data.len()),
        "account must be rent-exempt at its new size: {} lamports for {} bytes",
        grown.lamports,
        grown.data.len()
    );

    let after = GlobalState::try_from(&grown.data[..]).unwrap();
    assert_eq!(after.ip_verifier_authority_pk, verifier.pubkey());

    // The rest of the account survived the grow.
    assert_eq!(after.activator_authority_pk, current.activator_authority_pk);
    assert_eq!(after.sentinel_authority_pk, current.sentinel_authority_pk);
    assert_eq!(after.health_oracle_pk, current.health_oracle_pk);
    assert_eq!(after.feed_authority_pk, current.feed_authority_pk);
    assert_eq!(after.foundation_allowlist, current.foundation_allowlist);
    assert_eq!(after.qa_allowlist, current.qa_allowlist);
    assert_eq!(after.feature_flags, current.feature_flags);
    assert_eq!(after.account_index, current.account_index);
}
