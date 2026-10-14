//! A payer's Permission PDA that has never been created (or was closed) is treated by
//! `authorize()` exactly like no Permission account at all, so clients can append it
//! unconditionally and offline. These tests drive the real program through an
//! `authorize()`-gated instruction with that uninitialized PDA as the trailing account.

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::*,
    pda::get_permission_pda,
    processors::{
        allowlist::qa::add::AddQaAllowlistArgs, globalstate::setfeatureflags::SetFeatureFlagsArgs,
        permission::create::PermissionCreateArgs,
    },
    state::{feature_flags::FeatureFlag, permission::permission_flags},
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

/// `AddQaAllowlist` (GLOBALSTATE_ADMIN, legacy: foundation allowlist) with `permission`
/// as the trailing account after `[payer, system]` — the layout the SDK emits.
async fn add_qa_allowlist(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &Keypair,
    permission: Pubkey,
) -> Result<(), BanksClientError> {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction_with_extra_accounts(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddQaAllowlist(AddQaAllowlistArgs {
            pubkey: Pubkey::new_unique(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        payer,
        &[AccountMeta::new_readonly(permission, false)],
    )
    .await
}

async fn enable_strict_mode(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &Keypair,
) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::RequirePermissionAccounts.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        payer,
    )
    .await;
}

#[tokio::test]
async fn test_uninitialized_permission_pda_strict_off_falls_back_to_legacy() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    let (permission_pda, _) = get_permission_pda(&program_id, &payer.pubkey());
    assert!(account_is_closed(&mut banks_client, permission_pda).await);

    // Never-created PDA: the foundation payer is authorized via the legacy allowlist.
    add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap();

    // Pre-funded by a third party (still system-owned, no data): same outcome, so a
    // lamport transfer cannot be used to grief the payer into a hard failure.
    transfer(&mut banks_client, &payer, &permission_pda, 1_000_000).await;
    let account = banks_client
        .get_account(permission_pda)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(account.owner, solana_system_interface::program::ID);
    assert!(account.data.is_empty());
    add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap();

    // A key with no legacy authority is still denied.
    let outsider = Keypair::new();
    transfer(&mut banks_client, &payer, &outsider.pubkey(), 1_000_000_000).await;
    let (outsider_pda, _) = get_permission_pda(&program_id, &outsider.pubkey());
    let err = add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &outsider,
        outsider_pda,
    )
    .await
    .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);
}

#[tokio::test]
async fn test_uninitialized_permission_pda_wrong_pda_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    // Someone else's (uninitialized) Permission PDA is not the payer's.
    let (other_pda, _) = get_permission_pda(&program_id, &Pubkey::new_unique());

    let err = add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        other_pda,
    )
    .await
    .unwrap_err();
    assert!(
        matches!(
            err,
            BanksClientError::TransactionError(TransactionError::InstructionError(
                0,
                InstructionError::InvalidArgument
            ))
        ),
        "expected InvalidArgument, got {err:?}"
    );
}

#[tokio::test]
async fn test_uninitialized_permission_pda_strict_on() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    let (permission_pda, _) = get_permission_pda(&program_id, &payer.pubkey());

    enable_strict_mode(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    // Strict mode: the legacy allowlist no longer authorizes, with or without lamports.
    let err = add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    transfer(&mut banks_client, &payer, &permission_pda, 1_000_000).await;
    let err = add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    // ...except the foundation PERMISSION_ADMIN recovery: a foundation member can still
    // create Permission accounts — here its own, whose PDA is both the account being
    // created and the (still uninitialized, pre-funded) trailing Permission account.
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: payer.pubkey(),
            permissions: permission_flags::GLOBALSTATE_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
        &[AccountMeta::new_readonly(permission_pda, false)],
    )
    .await
    .unwrap();

    // With the Permission account now initialized, the same instruction succeeds.
    add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap();
}
