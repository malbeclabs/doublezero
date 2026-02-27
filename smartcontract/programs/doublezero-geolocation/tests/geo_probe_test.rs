#![allow(unused_mut)]

mod test_helpers;

use doublezero_geolocation::{
    error::GeolocationError,
    instructions::GeolocationInstruction,
    pda::get_geo_probe_pda,
    processors::geo_probe::{create::CreateGeoProbeArgs, update::UpdateGeoProbeArgs},
    serviceability_program_id,
    state::{accounttype::AccountType, geo_probe::GeoProbe},
};
use doublezero_serviceability::state::exchange::ExchangeStatus;
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, Instruction, InstructionError},
    pubkey::Pubkey,
    signature::Keypair,
    transaction::{Transaction, TransactionError},
};
use std::net::Ipv4Addr;
use test_helpers::setup_test_with_exchange;

#[tokio::test]
async fn test_create_geo_probe_success() {
    let (mut banks_client, program_id, recent_blockhash, payer_pubkey, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    // Create GeoProbe
    let code = "probe-ams-01";
    let (probe_pda, _) = get_geo_probe_pda(&program_id, code);
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(&program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    let args = CreateGeoProbeArgs {
        code: code.to_string(),
        public_ip: Ipv4Addr::new(8, 8, 8, 8),
        location_offset_port: 4242,
        metrics_publisher_pk: Pubkey::new_unique(),
    };

    let ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::CreateGeoProbe(args.clone()),
        vec![
            AccountMeta::new(probe_pda, false),
            AccountMeta::new_readonly(exchange_pubkey, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(payer_pubkey, true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    );

    // Use a deterministic keypair for the test payer
    let payer = Keypair::from_bytes(&[0u8; 64]).unwrap();
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer_pubkey),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Verify the account was created
    let probe_account = banks_client.get_account(probe_pda).await.unwrap().unwrap();
    let probe = GeoProbe::try_from(&probe_account.data[..]).unwrap();

    assert_eq!(probe.account_type, AccountType::GeoProbe);
    assert_eq!(probe.owner, payer_pubkey);
    assert_eq!(probe.exchange_pk, exchange_pubkey);
    assert_eq!(probe.public_ip, Ipv4Addr::new(8, 8, 8, 8));
    assert_eq!(probe.location_offset_port, 4242);
    assert_eq!(probe.metrics_publisher_pk, args.metrics_publisher_pk);
    assert_eq!(probe.reference_count, 0);
    assert_eq!(probe.code, code);
    assert_eq!(probe.parent_devices.len(), 0);
}

#[tokio::test]
async fn test_create_geo_probe_invalid_code_length() {
    let (mut banks_client, program_id, recent_blockhash, payer_pubkey, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    // Try to create GeoProbe with code that's too long
    let code = "a".repeat(33); // Exceeds 32 char limit
    let (probe_pda, _) = get_geo_probe_pda(&program_id, &code);
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(&program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    let args = CreateGeoProbeArgs {
        code,
        public_ip: Ipv4Addr::new(8, 8, 8, 8),
        location_offset_port: 4242,
        metrics_publisher_pk: Pubkey::new_unique(),
    };

    let ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::CreateGeoProbe(args),
        vec![
            AccountMeta::new(probe_pda, false),
            AccountMeta::new_readonly(exchange_pubkey, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(payer_pubkey, true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    );

    let payer = Keypair::from_bytes(&[0u8; 64]).unwrap();
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer_pubkey),
        &[&payer],
        *recent_blockhash.read().await,
    );

    let result = banks_client.process_transaction(tx).await;
    assert!(result.is_err());

    let err = result.unwrap_err().unwrap();
    match err {
        TransactionError::InstructionError(0, InstructionError::Custom(code)) => {
            assert_eq!(code, GeolocationError::InvalidCodeLength as u32);
        }
        _ => panic!("Expected InvalidCodeLength error, got: {:?}", err),
    }
}

#[tokio::test]
async fn test_create_geo_probe_exchange_not_activated() {
    let (mut banks_client, program_id, recent_blockhash, payer_pubkey, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Pending).await;

    let code = "probe-pending";
    let (probe_pda, _) = get_geo_probe_pda(&program_id, code);
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(&program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    let args = CreateGeoProbeArgs {
        code: code.to_string(),
        public_ip: Ipv4Addr::new(8, 8, 8, 8),
        location_offset_port: 4242,
        metrics_publisher_pk: Pubkey::new_unique(),
    };

    let ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::CreateGeoProbe(args),
        vec![
            AccountMeta::new(probe_pda, false),
            AccountMeta::new_readonly(exchange_pubkey, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(payer_pubkey, true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    );

    let payer = Keypair::from_bytes(&[0u8; 64]).unwrap();
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer_pubkey),
        &[&payer],
        *recent_blockhash.read().await,
    );

    let result = banks_client.process_transaction(tx).await;
    assert!(result.is_err());
    // Exchange not activated should return InvalidAccountData
}

#[tokio::test]
async fn test_update_geo_probe_success() {
    let (mut banks_client, program_id, recent_blockhash, payer_pubkey, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    // First create a GeoProbe
    let code = "probe-update";
    let (probe_pda, _) = get_geo_probe_pda(&program_id, code);
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(&program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    // Create probe first
    let create_args = CreateGeoProbeArgs {
        code: code.to_string(),
        public_ip: Ipv4Addr::new(8, 8, 8, 8),
        location_offset_port: 4242,
        metrics_publisher_pk: Pubkey::new_unique(),
    };

    let create_ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::CreateGeoProbe(create_args.clone()),
        vec![
            AccountMeta::new(probe_pda, false),
            AccountMeta::new_readonly(exchange_pubkey, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(payer_pubkey, true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    );

    let payer = Keypair::from_bytes(&[0u8; 64]).unwrap();
    let tx = Transaction::new_signed_with_payer(
        &[create_ix],
        Some(&payer_pubkey),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Now update the probe
    let new_metrics_publisher = Pubkey::new_unique();
    let update_args = UpdateGeoProbeArgs {
        public_ip: Some(Ipv4Addr::new(1, 1, 1, 1)),
        location_offset_port: Some(5353),
        metrics_publisher_pk: Some(new_metrics_publisher),
    };

    let update_ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::UpdateGeoProbe(update_args.clone()),
        vec![
            AccountMeta::new(probe_pda, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(payer_pubkey, true),
        ],
    );

    let tx = Transaction::new_signed_with_payer(
        &[update_ix],
        Some(&payer_pubkey),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Verify the update
    let probe_account = banks_client.get_account(probe_pda).await.unwrap().unwrap();
    let probe = GeoProbe::try_from(&probe_account.data[..]).unwrap();

    assert_eq!(probe.public_ip, Ipv4Addr::new(1, 1, 1, 1));
    assert_eq!(probe.location_offset_port, 5353);
    assert_eq!(probe.metrics_publisher_pk, new_metrics_publisher);
    // Verify immutable fields didn't change
    assert_eq!(probe.code, code);
    assert_eq!(probe.exchange_pk, exchange_pubkey);
}

#[tokio::test]
async fn test_delete_geo_probe_success() {
    let (mut banks_client, program_id, recent_blockhash, payer_pubkey, exchange_pubkey) =
        setup_test_with_exchange(ExchangeStatus::Activated).await;

    // First create a GeoProbe
    let code = "probe-delete";
    let (probe_pda, _) = get_geo_probe_pda(&program_id, code);
    let program_config_pda = doublezero_geolocation::pda::get_program_config_pda(&program_id).0;
    let serviceability_globalstate_pda =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;

    // Create probe first
    let create_args = CreateGeoProbeArgs {
        code: code.to_string(),
        public_ip: Ipv4Addr::new(8, 8, 8, 8),
        location_offset_port: 4242,
        metrics_publisher_pk: Pubkey::new_unique(),
    };

    let create_ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::CreateGeoProbe(create_args),
        vec![
            AccountMeta::new(probe_pda, false),
            AccountMeta::new_readonly(exchange_pubkey, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(payer_pubkey, true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    );

    let payer = Keypair::from_bytes(&[0u8; 64]).unwrap();
    let tx = Transaction::new_signed_with_payer(
        &[create_ix],
        Some(&payer_pubkey),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Now delete the probe
    let delete_ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::DeleteGeoProbe,
        vec![
            AccountMeta::new(probe_pda, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(serviceability_globalstate_pda, false),
            AccountMeta::new(payer_pubkey, true),
        ],
    );

    let tx = Transaction::new_signed_with_payer(
        &[delete_ix],
        Some(&payer_pubkey),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    // Verify the account was deleted
    let probe_account = banks_client.get_account(probe_pda).await.unwrap();
    assert!(probe_account.is_none());
}
