#![allow(unused_mut)]

mod test_helpers;

use doublezero_geolocation::{
    entrypoint::process_instruction,
    error::GeolocationError,
    instructions::GeolocationInstruction,
    pda::{get_geo_probe_pda, get_geolocation_user_pda},
    processors::{
        geo_probe::create::CreateGeoProbeArgs,
        geolocation_user::{
            add_target::AddTargetArgs, create::CreateGeolocationUserArgs,
            remove_target::RemoveTargetArgs, set_result_destination::SetResultDestinationArgs,
            update::UpdateGeolocationUserArgs, update_payment_status::UpdatePaymentStatusArgs,
        },
    },
    serviceability_program_id,
    state::{
        accounttype::AccountType,
        geo_probe::GeoProbe,
        geolocation_user::{
            GeoLocationTargetType, GeolocationBillingConfig, GeolocationPaymentStatus,
            GeolocationUser, GeolocationUserStatus,
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
use std::net::Ipv4Addr;
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
        result_destination: String::new(),
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

// --- Helpers for AddTarget / RemoveTarget / UpdatePaymentStatus tests ---

/// Creates a GeoProbe via instruction (requires foundation setup).
async fn create_geo_probe(
    banks_client: &mut BanksClient,
    program_id: &Pubkey,
    recent_blockhash: &tokio::sync::RwLock<solana_sdk::hash::Hash>,
    payer: &Keypair,
    exchange_pubkey: &Pubkey,
    probe_code: &str,
) -> Pubkey {
    let (probe_pda, _) = get_geo_probe_pda(program_id, probe_code);
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    let ix = Instruction::new_with_borsh(
        *program_id,
        &GeolocationInstruction::CreateGeoProbe(CreateGeoProbeArgs {
            code: probe_code.to_string(),
            public_ip: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 4242,
            metrics_publisher_pk: Pubkey::new_unique(),
        }),
        vec![
            AccountMeta::new(probe_pda, false),
            AccountMeta::new_readonly(*exchange_pubkey, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    );

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();
    probe_pda
}

fn build_add_target_ix(
    program_id: &Pubkey,
    user_pda: &Pubkey,
    probe_pda: &Pubkey,
    payer: &Pubkey,
    args: AddTargetArgs,
) -> Instruction {
    Instruction::new_with_borsh(
        *program_id,
        &GeolocationInstruction::AddTarget(args),
        vec![
            AccountMeta::new(*user_pda, false),
            AccountMeta::new(*probe_pda, false),
            AccountMeta::new(*payer, true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    )
}

fn build_remove_target_ix(
    program_id: &Pubkey,
    user_pda: &Pubkey,
    probe_pda: &Pubkey,
    payer: &Pubkey,
    args: RemoveTargetArgs,
) -> Instruction {
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    Instruction::new_with_borsh(
        *program_id,
        &GeolocationInstruction::RemoveTarget(args),
        vec![
            AccountMeta::new(*user_pda, false),
            AccountMeta::new(*probe_pda, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(*payer, true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    )
}

// --- AddTarget tests ---

#[tokio::test]
async fn test_add_target_outbound_success() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-add-out",
    )
    .await;

    let user_code = "user-add-out";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);
    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        },
    );

    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Verify target was added
    let account = banks_client.get_account(user_pda).await.unwrap().unwrap();
    let user = GeolocationUser::try_from(&account.data[..]).unwrap();
    assert_eq!(user.targets.len(), 1);
    assert_eq!(user.targets[0].target_type, GeoLocationTargetType::Outbound);
    assert_eq!(user.targets[0].ip_address, Ipv4Addr::new(8, 8, 8, 8));
    assert_eq!(user.targets[0].geoprobe_pk, probe_pda);

    // Verify probe reference_count and target_target_update_count incremented
    let probe_account = banks_client.get_account(probe_pda).await.unwrap().unwrap();
    let probe = GeoProbe::try_from(&probe_account.data[..]).unwrap();
    assert_eq!(probe.reference_count, 1);
    assert_eq!(probe.target_update_count, 1);

    // Also add an inbound target to verify both target types work
    let inbound_target_pk = Pubkey::new_unique();
    let add_inbound_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::Inbound,
            ip_address: Ipv4Addr::UNSPECIFIED,
            location_offset_port: 0,
            target_pk: inbound_target_pk,
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[add_inbound_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let account = banks_client.get_account(user_pda).await.unwrap().unwrap();
    let user = GeolocationUser::try_from(&account.data[..]).unwrap();
    assert_eq!(user.targets.len(), 2);
    assert_eq!(user.targets[1].target_type, GeoLocationTargetType::Inbound);
    assert_eq!(user.targets[1].target_pk, inbound_target_pk);

    let probe_account = banks_client.get_account(probe_pda).await.unwrap().unwrap();
    let probe = GeoProbe::try_from(&probe_account.data[..]).unwrap();
    assert_eq!(probe.reference_count, 2);
    assert_eq!(probe.target_update_count, 2);
}

#[tokio::test]
async fn test_add_target_outbound_invalid_ip() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-bad-ip",
    )
    .await;

    let user_code = "user-bad-ip";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);
    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(10, 0, 0, 1), // private IP
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        },
    );

    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::Custom(code)) => {
            assert_eq!(code, GeolocationError::InvalidIpAddress as u32);
        }
        _ => panic!("Expected InvalidIpAddress error, got: {:?}", err),
    }
}

#[tokio::test]
async fn test_add_target_duplicate_rejected() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-dup-tgt",
    )
    .await;

    let user_code = "user-dup-tgt";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);
    let args = AddTargetArgs {
        target_type: GeoLocationTargetType::Outbound,
        ip_address: Ipv4Addr::new(1, 1, 1, 1),
        location_offset_port: 8923,
        target_pk: Pubkey::default(),
    };

    // First add succeeds
    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        args.clone(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Second add with same target identity but different location_offset_port (different tx bytes,
    // but the duplicate check matches on target_type + geoprobe_pk + ip_address + target_pk).
    let mut args2 = args;
    args2.location_offset_port = 9999;
    let add_ix2 = build_add_target_ix(&program_id, &user_pda, &probe_pda, &payer.pubkey(), args2);
    let tx = Transaction::new_signed_with_payer(
        &[add_ix2],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::Custom(code)) => {
            assert_eq!(code, GeolocationError::TargetAlreadyExists as u32);
        }
        _ => panic!("Expected TargetAlreadyExists error, got: {:?}", err),
    }
}

#[tokio::test]
async fn test_add_target_outbound_icmp_success() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-add-icmp",
    )
    .await;

    let user_code = "user-add-icmp";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);
    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::OutboundIcmp,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let account = banks_client.get_account(user_pda).await.unwrap().unwrap();
    let user = GeolocationUser::try_from(&account.data[..]).unwrap();
    assert_eq!(user.targets.len(), 1);
    assert_eq!(
        user.targets[0].target_type,
        GeoLocationTargetType::OutboundIcmp
    );
    assert_eq!(user.targets[0].ip_address, Ipv4Addr::new(8, 8, 8, 8));
    assert_eq!(user.targets[0].location_offset_port, 8923);
    assert_eq!(user.targets[0].geoprobe_pk, probe_pda);

    let probe_account = banks_client.get_account(probe_pda).await.unwrap().unwrap();
    let probe = GeoProbe::try_from(&probe_account.data[..]).unwrap();
    assert_eq!(probe.reference_count, 1);
    assert_eq!(probe.target_update_count, 1);
}

#[tokio::test]
async fn test_add_target_outbound_icmp_invalid_ip() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-icmp-bad",
    )
    .await;

    let user_code = "user-icmp-bad";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);
    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::OutboundIcmp,
            ip_address: Ipv4Addr::new(10, 0, 0, 1),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::Custom(code)) => {
            assert_eq!(code, GeolocationError::InvalidIpAddress as u32);
        }
        _ => panic!("Expected InvalidIpAddress error, got: {:?}", err),
    }
}

// --- RemoveTarget tests ---

#[tokio::test]
async fn test_remove_target_success() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-rm",
    )
    .await;

    let user_code = "user-rm";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    // Add a target
    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Verify reference_count is 1
    let probe_account = banks_client.get_account(probe_pda).await.unwrap().unwrap();
    let probe = GeoProbe::try_from(&probe_account.data[..]).unwrap();
    assert_eq!(probe.reference_count, 1);

    // Remove the target
    let rm_ix = build_remove_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        RemoveTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[rm_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Verify target removed
    let account = banks_client.get_account(user_pda).await.unwrap().unwrap();
    let user = GeolocationUser::try_from(&account.data[..]).unwrap();
    assert!(user.targets.is_empty());

    // Verify reference_count decremented and target_update_count incremented
    let probe_account = banks_client.get_account(probe_pda).await.unwrap().unwrap();
    let probe = GeoProbe::try_from(&probe_account.data[..]).unwrap();
    assert_eq!(probe.reference_count, 0);
    assert_eq!(probe.target_update_count, 2); // 1 from add + 1 from remove
}

#[tokio::test]
async fn test_remove_target_outbound_icmp_success() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-rm-icmp",
    )
    .await;

    let user_code = "user-rm-icmp";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::OutboundIcmp,
            ip_address: Ipv4Addr::new(8, 8, 4, 4),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let probe_account = banks_client.get_account(probe_pda).await.unwrap().unwrap();
    let probe = GeoProbe::try_from(&probe_account.data[..]).unwrap();
    assert_eq!(probe.reference_count, 1);

    let rm_ix = build_remove_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        RemoveTargetArgs {
            target_type: GeoLocationTargetType::OutboundIcmp,
            ip_address: Ipv4Addr::new(8, 8, 4, 4),
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[rm_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let account = banks_client.get_account(user_pda).await.unwrap().unwrap();
    let user = GeolocationUser::try_from(&account.data[..]).unwrap();
    assert!(user.targets.is_empty());

    let probe_account = banks_client.get_account(probe_pda).await.unwrap().unwrap();
    let probe = GeoProbe::try_from(&probe_account.data[..]).unwrap();
    assert_eq!(probe.reference_count, 0);
    assert_eq!(probe.target_update_count, 2);
}

#[tokio::test]
async fn test_remove_target_not_found() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-rm-nf",
    )
    .await;

    let user_code = "user-rm-nf";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);
    let rm_ix = build_remove_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        RemoveTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[rm_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::Custom(code)) => {
            assert_eq!(code, GeolocationError::TargetNotFound as u32);
        }
        _ => panic!("Expected TargetNotFound error, got: {:?}", err),
    }
}

#[tokio::test]
async fn test_remove_target_foundation_can_remove() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-rm-f",
    )
    .await;

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

    let user_code = "user-rm-f";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &user_owner.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&user_owner.pubkey()),
        &[&user_owner],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    // Owner adds a target
    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &user_owner.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&user_owner.pubkey()),
        &[&user_owner],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Foundation (payer) removes the target
    let rm_ix = build_remove_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        RemoveTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[rm_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let account = banks_client.get_account(user_pda).await.unwrap().unwrap();
    let user = GeolocationUser::try_from(&account.data[..]).unwrap();
    assert!(user.targets.is_empty());
}

#[tokio::test]
async fn test_remove_target_unauthorized_non_owner() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-rm-u",
    )
    .await;

    let user_code = "user-rm-u";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    // Owner (payer) adds a target
    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Random non-owner, non-foundation signer tries to remove
    let random_signer = Keypair::new();
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(&program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    let rm_ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::RemoveTarget(RemoveTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            target_pk: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(user_pda, false),
            AccountMeta::new(probe_pda, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(random_signer.pubkey(), true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    );
    let tx = Transaction::new_signed_with_payer(
        &[rm_ix],
        Some(&payer.pubkey()),
        &[&payer, &random_signer],
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

// --- UpdatePaymentStatus tests ---

#[tokio::test]
async fn test_update_payment_status_success() {
    let (mut banks_client, program_id, recent_blockhash, payer, _) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let user_code = "user-pay-ok";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(&program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    let update_ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::UpdatePaymentStatus(UpdatePaymentStatusArgs {
            payment_status: GeolocationPaymentStatus::Paid,
            last_deduction_dz_epoch: Some(42),
        }),
        vec![
            AccountMeta::new(user_pda, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
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
    assert_eq!(user.payment_status, GeolocationPaymentStatus::Paid);
    match user.billing {
        GeolocationBillingConfig::FlatPerEpoch(config) => {
            assert_eq!(config.last_deduction_dz_epoch, 42);
        }
    }
}

#[tokio::test]
async fn test_update_payment_status_invalid_value() {
    let (mut banks_client, program_id, recent_blockhash, payer, _) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let user_code = "user-pay-inv";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(&program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    // Serialize a valid instruction, then patch the payment_status byte to an
    // invalid value. With GeolocationPaymentStatus as the field type, invalid
    // values are rejected at Borsh deserialization time (InvalidInstructionData).
    let mut data = borsh::to_vec(&GeolocationInstruction::UpdatePaymentStatus(
        UpdatePaymentStatusArgs {
            payment_status: GeolocationPaymentStatus::Delinquent,
            last_deduction_dz_epoch: None,
        },
    ))
    .unwrap();
    data[1] = 99; // patch payment_status to invalid discriminant

    let update_ix = Instruction {
        program_id,
        accounts: vec![
            AccountMeta::new(user_pda, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(payer.pubkey(), true),
        ],
        data,
    };

    let tx = Transaction::new_signed_with_payer(
        &[update_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::InvalidInstructionData) => {}
        _ => panic!("Expected InvalidInstructionData error, got: {:?}", err),
    }
}

// --- SetResultDestination tests ---

fn build_set_result_destination_ix(
    program_id: &Pubkey,
    user_pda: &Pubkey,
    payer: &Pubkey,
    probe_pdas: &[Pubkey],
    args: SetResultDestinationArgs,
) -> Instruction {
    let mut accounts = vec![AccountMeta::new(*user_pda, false)];
    for probe_pda in probe_pdas {
        accounts.push(AccountMeta::new(*probe_pda, false));
    }
    accounts.push(AccountMeta::new(*payer, true));
    accounts.push(AccountMeta::new_readonly(
        solana_program::system_program::id(),
        false,
    ));
    Instruction::new_with_borsh(
        *program_id,
        &GeolocationInstruction::SetResultDestination(args),
        accounts,
    )
}

#[tokio::test]
async fn test_set_result_destination_success() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-srd-ok",
    )
    .await;

    let user_code = "user-srd-ok";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    // Add an outbound target
    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Set result destination
    let set_ix = build_set_result_destination_ix(
        &program_id,
        &user_pda,
        &payer.pubkey(),
        &[probe_pda],
        SetResultDestinationArgs {
            destination: "185.199.108.1:9000".to_string(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[set_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Verify user fields updated
    let account = banks_client.get_account(user_pda).await.unwrap().unwrap();
    let user = GeolocationUser::try_from(&account.data[..]).unwrap();
    assert_eq!(user.result_destination, "185.199.108.1:9000");

    // Verify probe target_update_count bumped (1 from add + 1 from set_result_destination)
    let probe_account = banks_client.get_account(probe_pda).await.unwrap().unwrap();
    let probe = GeoProbe::try_from(&probe_account.data[..]).unwrap();
    assert_eq!(probe.target_update_count, 2);
}

#[tokio::test]
async fn test_set_result_destination_clear() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-srd-clr",
    )
    .await;

    let user_code = "user-srd-clr";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    // Add an outbound target
    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Set a destination first
    let set_ix = build_set_result_destination_ix(
        &program_id,
        &user_pda,
        &payer.pubkey(),
        &[probe_pda],
        SetResultDestinationArgs {
            destination: "185.199.108.1:9000".to_string(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[set_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Clear it with empty string
    let clear_ix = build_set_result_destination_ix(
        &program_id,
        &user_pda,
        &payer.pubkey(),
        &[probe_pda],
        SetResultDestinationArgs {
            destination: String::new(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[clear_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Verify fields reset
    let account = banks_client.get_account(user_pda).await.unwrap().unwrap();
    let user = GeolocationUser::try_from(&account.data[..]).unwrap();
    assert_eq!(user.result_destination, "");
}

#[tokio::test]
async fn test_set_result_destination_unauthorized() {
    let (mut banks_client, program_id, recent_blockhash, payer) = setup_test().await;

    let user_code = "user-srd-auth";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    // Fund a wrong signer
    let wrong_signer = Keypair::new();
    let transfer_ix = solana_program::system_instruction::transfer(
        &payer.pubkey(),
        &wrong_signer.pubkey(),
        1_000_000_000,
    );
    let tx = Transaction::new_signed_with_payer(
        &[transfer_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Try set_result_destination with wrong signer (no targets, so no probe accounts needed)
    let set_ix = build_set_result_destination_ix(
        &program_id,
        &user_pda,
        &wrong_signer.pubkey(),
        &[],
        SetResultDestinationArgs {
            destination: "185.199.108.1:9000".to_string(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[set_ix],
        Some(&wrong_signer.pubkey()),
        &[&wrong_signer],
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
async fn test_set_result_destination_invalid_ip() {
    let (mut banks_client, program_id, recent_blockhash, payer) = setup_test().await;

    let user_code = "user-srd-badip";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    // Try with private IP (no targets, so no probe accounts needed)
    let set_ix = build_set_result_destination_ix(
        &program_id,
        &user_pda,
        &payer.pubkey(),
        &[],
        SetResultDestinationArgs {
            destination: "10.0.0.1:9000".to_string(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[set_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::Custom(code)) => {
            assert_eq!(code, GeolocationError::InvalidIpAddress as u32);
        }
        _ => panic!("Expected InvalidIpAddress error, got: {:?}", err),
    }
}

#[tokio::test]
async fn test_set_result_destination_no_targets() {
    let (mut banks_client, program_id, recent_blockhash, payer) = setup_test().await;

    let user_code = "user-srd-notgt";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    // Set destination with no targets and no probe accounts
    let set_ix = build_set_result_destination_ix(
        &program_id,
        &user_pda,
        &payer.pubkey(),
        &[],
        SetResultDestinationArgs {
            destination: "185.199.108.1:9000".to_string(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[set_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Verify fields set
    let account = banks_client.get_account(user_pda).await.unwrap().unwrap();
    let user = GeolocationUser::try_from(&account.data[..]).unwrap();
    assert_eq!(user.result_destination, "185.199.108.1:9000");
}

#[tokio::test]
async fn test_set_result_destination_domain() {
    let (mut banks_client, program_id, recent_blockhash, payer) = setup_test().await;

    let user_code = "user-srd-dom";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    let set_ix = build_set_result_destination_ix(
        &program_id,
        &user_pda,
        &payer.pubkey(),
        &[],
        SetResultDestinationArgs {
            destination: "results.example.com:9000".to_string(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[set_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let account = banks_client.get_account(user_pda).await.unwrap().unwrap();
    let user = GeolocationUser::try_from(&account.data[..]).unwrap();
    assert_eq!(user.result_destination, "results.example.com:9000");
}

#[tokio::test]
async fn test_set_result_destination_invalid_format() {
    let (mut banks_client, program_id, recent_blockhash, payer) = setup_test().await;

    let user_code = "user-srd-badfmt";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    let set_ix = build_set_result_destination_ix(
        &program_id,
        &user_pda,
        &payer.pubkey(),
        &[],
        SetResultDestinationArgs {
            destination: "no-port".to_string(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[set_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::InvalidInstructionData) => {}
        _ => panic!("Expected InvalidInstructionData error, got: {:?}", err),
    }
}

#[tokio::test]
async fn test_set_result_destination_unrelated_probe() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-srd-rel",
    )
    .await;

    let unrelated_probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-srd-unrel",
    )
    .await;

    let user_code = "user-srd-unrel";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Pass unrelated_probe_pda instead of probe_pda
    let set_ix = build_set_result_destination_ix(
        &program_id,
        &user_pda,
        &payer.pubkey(),
        &[unrelated_probe_pda],
        SetResultDestinationArgs {
            destination: "185.199.108.1:9000".to_string(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[set_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData) => {}
        _ => panic!("Expected InvalidAccountData error, got: {:?}", err),
    }
}

#[tokio::test]
async fn test_set_result_destination_wrong_probe_count() {
    let (mut banks_client, program_id, recent_blockhash, payer, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    let probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-srd-cnt",
    )
    .await;

    let extra_probe_pda = create_geo_probe(
        &mut banks_client,
        &program_id,
        &recent_blockhash,
        &payer,
        &exchange_pubkey,
        "probe-srd-extra",
    )
    .await;

    let user_code = "user-srd-cnt";
    let ix = build_create_user_ix(
        &program_id,
        user_code,
        &Pubkey::new_unique(),
        &payer.pubkey(),
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let (user_pda, _) = get_geolocation_user_pda(&program_id, user_code);

    let add_ix = build_add_target_ix(
        &program_id,
        &user_pda,
        &probe_pda,
        &payer.pubkey(),
        AddTargetArgs {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[add_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Pass two probe accounts when user only has one unique probe
    let set_ix = build_set_result_destination_ix(
        &program_id,
        &user_pda,
        &payer.pubkey(),
        &[probe_pda, extra_probe_pda],
        SetResultDestinationArgs {
            destination: "185.199.108.1:9000".to_string(),
        },
    );
    let tx = Transaction::new_signed_with_payer(
        &[set_ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    let result = banks_client.process_transaction(tx).await;
    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::Custom(code)) => {
            assert_eq!(code, GeolocationError::ProbeAccountCountMismatch as u32);
        }
        _ => panic!("Expected ProbeAccountCountMismatch error, got: {:?}", err),
    }
}
