//! Integration tests for onchain allocation for CreateDevice
//!
//! These tests verify that Devices can be atomically created+activated using
//! ResourceExtension accounts (TunnelIds + DzPrefixBlocks) in a single instruction.

use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs, device::create::DeviceCreateArgs,
        exchange::create::ExchangeCreateArgs, globalstate::setfeatureflags::SetFeatureFlagsArgs,
        location::create::LocationCreateArgs,
    },
    resource::ResourceType,
    state::{device::*, feature_flags::FeatureFlag},
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signer::Signer};

mod test_helpers;
use test_helpers::*;

/// Helper: create location, exchange, and contributor prerequisites.
/// Returns (location_pubkey, exchange_pubkey, contributor_pubkey).
async fn setup_device_prerequisites(
    banks_client: &mut BanksClient,
    recent_blockhash: solana_program::hash::Hash,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    globalconfig_pubkey: Pubkey,
    payer: &solana_sdk::signature::Keypair,
) -> (Pubkey, Pubkey, Pubkey) {
    // Create Location
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            country: "us".to_string(),
            lat: 1.234,
            lng: 4.567,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Create Exchange
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            lat: 1.234,
            lng: 4.567,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    // Create Contributor
    let globalstate_account = get_globalstate(banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "cont".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    (location_pubkey, exchange_pubkey, contributor_pubkey)
}

/// Test atomic create+activate with onchain allocation (1 TunnelIds + 1 DzPrefixBlock)
#[tokio::test]
async fn test_create_device_atomic_with_onchain_allocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation feature flag
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let (location_pubkey, exchange_pubkey, contributor_pubkey) = setup_device_prerequisites(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "dz1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(),
            dz_prefixes: "110.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify device is Activated
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    assert_eq!(device.status, DeviceStatus::Activated);
    assert_eq!(device.code, "dz1");

    // Verify resource accounts exist
    let tunnel_ids = get_resource_extension_data(&mut banks_client, tunnel_ids_pda).await;
    assert!(tunnel_ids.is_some(), "TunnelIds resource should exist");

    let dz_prefix = get_resource_extension_data(&mut banks_client, dz_prefix_pda).await;
    assert!(dz_prefix.is_some(), "DzPrefixBlock resource should exist");

    println!("test_create_device_atomic_with_onchain_allocation PASSED");
}

/// Test atomic create with multiple dz_prefixes (1 TunnelIds + 2 DzPrefixBlocks)
#[tokio::test]
async fn test_create_device_atomic_multiple_dz_prefixes() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation feature flag
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let (location_pubkey, exchange_pubkey, contributor_pubkey) = setup_device_prerequisites(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix0_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));
    let (dz_prefix1_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 1));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "dz1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(),
            dz_prefixes: "110.1.0.0/23,110.2.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 3,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix0_pda, false),
            AccountMeta::new(dz_prefix1_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify device is Activated
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    assert_eq!(device.status, DeviceStatus::Activated);

    // Verify all resource accounts exist
    let tunnel_ids = get_resource_extension_data(&mut banks_client, tunnel_ids_pda).await;
    assert!(tunnel_ids.is_some(), "TunnelIds resource should exist");

    let dz_prefix0 = get_resource_extension_data(&mut banks_client, dz_prefix0_pda).await;
    assert!(dz_prefix0.is_some(), "DzPrefixBlock 0 should exist");

    let dz_prefix1 = get_resource_extension_data(&mut banks_client, dz_prefix1_pda).await;
    assert!(dz_prefix1.is_some(), "DzPrefixBlock 1 should exist");

    println!("test_create_device_atomic_multiple_dz_prefixes PASSED");
}
