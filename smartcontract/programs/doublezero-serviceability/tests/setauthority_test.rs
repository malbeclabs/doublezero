//! SetAuthority coverage for `ip_verifier_authority_pk` (RFC-27, issue #4196).
//!
//! The verifier public key is the trust root for IP ownership proof validation, so
//! it has to be rotatable onchain without a program upgrade, and rotating it must
//! not disturb the other authorities.

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::*,
    processors::globalstate::setauthority::SetAuthorityArgs,
};
use solana_program_test::*;
use solana_sdk::{
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
};

mod test_helpers;
use test_helpers::*;

/// Bring up a program instance with an initialized GlobalState.
async fn init_globalstate() -> (
    BanksClient,
    Pubkey,
    Keypair,
    solana_program::hash::Hash,
    Pubkey,
) {
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
    let (mut banks_client, program_id, payer, recent_blockhash, globalstate_pubkey) =
        init_globalstate().await;

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
    let (mut banks_client, program_id, payer, recent_blockhash, globalstate_pubkey) =
        init_globalstate().await;

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
