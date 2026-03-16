#![allow(unused_mut)]

mod test_helpers;

use doublezero_geolocation::{
    entrypoint::process_instruction,
    error::GeolocationError,
    instructions::GeolocationInstruction,
    pda::get_geolocation_user_pda,
    processors::geolocation_user::{
        create::CreateGeolocationUserArgs, update::UpdateGeolocationUserArgs,
    },
    serviceability_program_id,
    state::{
        accounttype::AccountType,
        geolocation_user::{
            GeolocationBillingConfig, GeolocationPaymentStatus, GeolocationUser,
            GeolocationUserStatus,
        },
    },
};
use doublezero_serviceability::state::exchange::ExchangeStatus;
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, Instruction, InstructionError},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::{Transaction, TransactionError},
};
use test_helpers::setup_test_with_exchange;

/// Minimal test setup for self-service instructions (no foundation allowlist needed).
async fn setup_test() -> (
    BanksClient,
    Pubkey,
    tokio::sync::RwLock<solana_sdk::hash::Hash>,
    Keypair,
) {
    let program_id = Pubkey::new_unique();
    let program_test = ProgramTest::new(
        "doublezero_geolocation",
        program_id,
        processor!(process_instruction),
    );

    let context = program_test.start_with_context().await;
    let recent_blockhash = tokio::sync::RwLock::new(context.last_blockhash);

    (
        context.banks_client,
        program_id,
        recent_blockhash,
        context.payer,
    )
}

fn build_create_user_ix(
    program_id: &Pubkey,
    code: &str,
    token_account: &Pubkey,
    payer: &Pubkey,
) -> Instruction {
    let (user_pda, _) = get_geolocation_user_pda(program_id, code);
    Instruction::new_with_borsh(
        *program_id,
        &GeolocationInstruction::CreateGeolocationUser(CreateGeolocationUserArgs {
            code: code.to_string(),
            token_account: *token_account,
        }),
        vec![
            AccountMeta::new(user_pda, false),
            AccountMeta::new(*payer, true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    )
}

#[tokio::test]
async fn test_create_geolocation_user_success() {
    let (mut banks_client, program_id, recent_blockhash, payer) = setup_test().await;

    let code = "geo-user-01";
    let token_account = Pubkey::new_unique();
    let ix = build_create_user_ix(&program_id, code, &token_account, &payer.pubkey());

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Verify account
    let (user_pda, _) = get_geolocation_user_pda(&program_id, code);
    let account = banks_client.get_account(user_pda).await.unwrap().unwrap();
    let user = GeolocationUser::try_from(&account.data[..]).unwrap();

    let expected = GeolocationUser {
        account_type: AccountType::GeolocationUser,
        owner: payer.pubkey(),
        code: code.to_string(),
        token_account,
        payment_status: GeolocationPaymentStatus::Delinquent,
        billing: GeolocationBillingConfig::default(),
        status: GeolocationUserStatus::Activated,
        targets: vec![],
    };
    assert_eq!(user, expected);
}

#[tokio::test]
async fn test_create_geolocation_user_invalid_code() {
    let (mut banks_client, program_id, recent_blockhash, payer) = setup_test().await;

    let code = "a".repeat(33);
    let token_account = Pubkey::new_unique();
    // Use truncated code for PDA derivation to avoid mismatch panic
    let (user_pda, _) = get_geolocation_user_pda(&program_id, &code[..32]);
    let ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::CreateGeolocationUser(CreateGeolocationUserArgs {
            code,
            token_account,
        }),
        vec![
            AccountMeta::new(user_pda, false),
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    );

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );

    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::Custom(code)) => {
            assert_eq!(code, GeolocationError::InvalidCodeLength as u32);
        }
        _ => panic!("Expected InvalidCodeLength error, got: {:?}", err),
    }
}

#[tokio::test]
async fn test_create_geolocation_user_duplicate() {
    let (mut banks_client, program_id, recent_blockhash, payer) = setup_test().await;

    let code = "geo-user-dup";
    let token_account = Pubkey::new_unique();
    let ix = build_create_user_ix(&program_id, code, &token_account, &payer.pubkey());

    // First create succeeds
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Second create with same code but different token_account (different tx bytes)
    let different_token = Pubkey::new_unique();
    let ix2 = build_create_user_ix(&program_id, code, &different_token, &payer.pubkey());
    let tx = Transaction::new_signed_with_payer(
        &[ix2],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::AccountAlreadyInitialized) => {}
        _ => panic!("Expected AccountAlreadyInitialized error, got: {:?}", err),
    }
}

#[tokio::test]
async fn test_update_geolocation_user_success() {
    let (mut banks_client, program_id, recent_blockhash, payer) = setup_test().await;

    let code = "geo-user-upd";
    let token_account = Pubkey::new_unique();
    let ix = build_create_user_ix(&program_id, code, &token_account, &payer.pubkey());

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Update token_account
    let new_token_account = Pubkey::new_unique();
    let (user_pda, _) = get_geolocation_user_pda(&program_id, code);
    let update_ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::UpdateGeolocationUser(UpdateGeolocationUserArgs {
            token_account: Some(new_token_account),
        }),
        vec![
            AccountMeta::new(user_pda, false),
            AccountMeta::new(payer.pubkey(), true),
        ],
    );

    let tx = Transaction::new_signed_with_payer(
        &[update_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let account = banks_client.get_account(user_pda).await.unwrap().unwrap();
    let user = GeolocationUser::try_from(&account.data[..]).unwrap();
    assert_eq!(user.token_account, new_token_account);
    assert_eq!(user.code, code);
}

#[tokio::test]
async fn test_update_geolocation_user_not_owner() {
    let (mut banks_client, program_id, recent_blockhash, payer) = setup_test().await;

    let code = "geo-user-auth";
    let token_account = Pubkey::new_unique();
    let ix = build_create_user_ix(&program_id, code, &token_account, &payer.pubkey());

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Try to update with a different signer
    let other_signer = Keypair::new();
    // Fund the other signer
    let transfer_ix = solana_sdk::system_instruction::transfer(
        &payer.pubkey(),
        &other_signer.pubkey(),
        1_000_000_000,
    );
    let tx = Transaction::new_signed_with_payer(
        &[transfer_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, code);
    let update_ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::UpdateGeolocationUser(UpdateGeolocationUserArgs {
            token_account: Some(Pubkey::new_unique()),
        }),
        vec![
            AccountMeta::new(user_pda, false),
            AccountMeta::new(other_signer.pubkey(), true),
        ],
    );

    let tx = Transaction::new_signed_with_payer(
        &[update_ix],
        Some(&other_signer.pubkey()),
        &[&other_signer],
        *recent_blockhash.read().await,
    );
    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::Custom(code)) => {
            assert_eq!(code, GeolocationError::Unauthorized as u32);
        }
        _ => panic!("Expected Unauthorized error, got: {:?}", err),
    }
}

#[tokio::test]
async fn test_delete_geolocation_user_not_owner_not_foundation() {
    let (mut banks_client, program_id, recent_blockhash, payer, _) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let code = "geo-user-delauth";
    let token_account = Pubkey::new_unique();
    let ix = build_create_user_ix(&program_id, code, &token_account, &payer.pubkey());

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let other_signer = Keypair::new();
    let transfer_ix = solana_sdk::system_instruction::transfer(
        &payer.pubkey(),
        &other_signer.pubkey(),
        1_000_000_000,
    );
    let tx = Transaction::new_signed_with_payer(
        &[transfer_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, code);
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(&program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    let delete_ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::DeleteGeolocationUser,
        vec![
            AccountMeta::new(user_pda, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(other_signer.pubkey(), true),
        ],
    );

    let tx = Transaction::new_signed_with_payer(
        &[delete_ix],
        Some(&other_signer.pubkey()),
        &[&other_signer],
        *recent_blockhash.read().await,
    );
    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::Custom(code)) => {
            assert_eq!(code, GeolocationError::NotAllowed as u32);
        }
        _ => panic!("Expected NotAllowed error, got: {:?}", err),
    }
}

#[tokio::test]
async fn test_delete_geolocation_user_success() {
    let (mut banks_client, program_id, recent_blockhash, payer, _) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let code = "geo-user-del";
    let token_account = Pubkey::new_unique();
    let ix = build_create_user_ix(&program_id, code, &token_account, &payer.pubkey());

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, code);
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(&program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    let delete_ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::DeleteGeolocationUser,
        vec![
            AccountMeta::new(user_pda, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(payer.pubkey(), true),
        ],
    );

    let tx = Transaction::new_signed_with_payer(
        &[delete_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let account = banks_client.get_account(user_pda).await.unwrap();
    assert!(account.is_none());
}

#[tokio::test]
async fn test_delete_geolocation_user_by_foundation() {
    let (mut banks_client, program_id, recent_blockhash, payer, _) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    // Create a user owned by a different keypair.
    let user_owner = Keypair::new();
    let transfer_ix = solana_sdk::system_instruction::transfer(
        &payer.pubkey(),
        &user_owner.pubkey(),
        2_000_000_000,
    );
    let tx = Transaction::new_signed_with_payer(
        &[transfer_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let code = "geo-user-fnd";
    let token_account = Pubkey::new_unique();
    let ix = build_create_user_ix(&program_id, code, &token_account, &user_owner.pubkey());

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&user_owner.pubkey()),
        &[&user_owner],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Foundation member (payer) deletes the user they don't own.
    let (user_pda, _) = get_geolocation_user_pda(&program_id, code);
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(&program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    let delete_ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::DeleteGeolocationUser,
        vec![
            AccountMeta::new(user_pda, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(payer.pubkey(), true),
        ],
    );

    let tx = Transaction::new_signed_with_payer(
        &[delete_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let account = banks_client.get_account(user_pda).await.unwrap();
    assert!(account.is_none());
}
